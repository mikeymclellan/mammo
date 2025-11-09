package cmd

import (
	"fmt"
	"sync"
	"time"

	"mammo/aliyuniot"
	"mammo/auth"
	"mammo/mammotion"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type position struct {
	x       float32
	y       float32
	angle   int32
	posType int32 // RTK fix quality: 4=Fixed, 5=Float, 1=DGPS, 0=GPS
}

type interactiveModel struct {
	cloudGateway     *aliyuniot.CloudIOTGateway
	device           *aliyuniot.Device
	position         position
	lastPosition     position
	positionUpdates  int
	batteryLevel     int
	status           string
	moveDistance     int32 // movement distance in mm (300mm = 30cm)
	speed            int32 // speed in mm/s
	ready            bool
	quitting         bool
	err              error
	stateManager     *mammotion.StateManager
	mowingDevice     *mammotion.MowingDevice
	positionChan     chan position
	batteryChan      chan int
}

type positionUpdateMsg position
type batteryUpdateMsg int
type readyMsg struct{}
type statusMsg string
type errMsg struct{ err error }

// waitForPositionUpdates waits for position updates and sends them as messages
func waitForPositionUpdates(positionChan chan position) tea.Cmd {
	return func() tea.Msg {
		pos := <-positionChan
		return positionUpdateMsg(pos)
	}
}

// waitForBatteryUpdates waits for battery updates and sends them as messages
func waitForBatteryUpdates(batteryChan chan int) tea.Cmd {
	return func() tea.Msg {
		battery := <-batteryChan
		return batteryUpdateMsg(battery)
	}
}

func (m interactiveModel) Init() tea.Cmd {
	return tea.Batch(
		waitForPositionUpdates(m.positionChan),
		waitForBatteryUpdates(m.batteryChan),
	)
}

func (m interactiveModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			m.quitting = true
			return m, tea.Quit

		case "up", "w":
			if m.ready {
				return m, m.moveForward()
			}

		case "down", "s":
			if m.ready {
				return m, m.moveBackward()
			}

		case "left", "a":
			if m.ready {
				return m, m.turnLeft()
			}

		case "right", "d":
			if m.ready {
				return m, m.turnRight()
			}

		case "space":
			if m.ready {
				return m, m.stop()
			}

		case "+", "=":
			m.moveDistance += 100
			if m.moveDistance > 3000 {
				m.moveDistance = 3000
			}

		case "-", "_":
			m.moveDistance -= 100
			if m.moveDistance < 100 {
				m.moveDistance = 100
			}
		}

	case readyMsg:
		m.ready = true
		m.status = "Connected and ready!"

	case positionUpdateMsg:
		m.lastPosition = m.position
		m.position = position(msg)
		m.positionUpdates++
		// Keep listening for more position updates
		return m, waitForPositionUpdates(m.positionChan)

	case batteryUpdateMsg:
		m.batteryLevel = int(msg)
		// Keep listening for more battery updates
		return m, waitForBatteryUpdates(m.batteryChan)

	case statusMsg:
		m.status = string(msg)

	case errMsg:
		m.err = msg.err
		return m, tea.Quit
	}

	return m, nil
}

func getRTKStatus(posType int32) string {
	switch posType {
	case 4:
		return "🟢 RTK Fixed"
	case 5:
		return "🟡 RTK Float"
	case 1:
		return "🟠 DGPS"
	case 0:
		return "🔴 GPS Only"
	default:
		return fmt.Sprintf("❓ Unknown (%d)", posType)
	}
}

func (m interactiveModel) View() string {
	if m.quitting {
		return "Disconnecting from mower...\n"
	}

	if m.err != nil {
		return fmt.Sprintf("Error: %v\n", m.err)
	}

	// Styles
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#00FF00")).
		Padding(1, 0)

	infoStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#FFFF00"))

	helpStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#888888")).
		Padding(1, 0)

	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#00FF00")).
		Padding(1, 2)

	// Build the UI
	var status string
	if m.ready {
		status = "🟢 READY"
	} else {
		status = "🟡 CONNECTING..."
	}

	// Normalize angle to 0-360 range
	normalizedAngle := m.position.angle % 360
	if normalizedAngle < 0 {
		normalizedAngle += 360
	}

	// Calculate position delta
	deltaX := m.position.x - m.lastPosition.x
	deltaY := m.position.y - m.lastPosition.y

	info := fmt.Sprintf(
		"%s\n\n"+
			"Position: X=%.0f Y=%.0f Angle=%d°\n"+
			"Delta: ΔX=%.0f ΔY=%.0f (Updates: %d)\n"+
			"GPS Fix: %s\n"+
			"Battery: %d%%\n"+
			"Move Distance: %dmm (%.1fm)\n"+
			"Speed: %d (±1000 typical)\n"+
			"Status: %s",
		status,
		m.position.x, m.position.y, normalizedAngle,
		deltaX, deltaY, m.positionUpdates,
		getRTKStatus(m.position.posType),
		m.batteryLevel,
		m.moveDistance, float32(m.moveDistance)/1000.0,
		m.speed,
		m.status,
	)

	controls := `Controls:
  ↑/W     - Move Forward
  ↓/S     - Move Backward
  ←/A     - Turn Left
  →/D     - Turn Right
  SPACE   - Emergency Stop
  +/-     - Adjust Move Distance (±100mm)
  Q       - Quit

Watch Delta values to see real-time movement tracking.`

	return titleStyle.Render("🤖 Mammotion Interactive Control") + "\n" +
		boxStyle.Render(infoStyle.Render(info)) + "\n" +
		helpStyle.Render(controls)
}

// Movement commands
func (m interactiveModel) moveForward() tea.Cmd {
	return tea.Sequence(
		func() tea.Msg {
			return statusMsg("⬆️  Sending forward command...")
		},
		func() tea.Msg {
			// Refresh session to ensure identityId is valid
			if err := m.cloudGateway.CheckOrRefreshSession(); err != nil {
				return errMsg{fmt.Errorf("session refresh failed: %w", err)}
			}

			// Send move command repeatedly every 200ms (device expects continuous updates)
			moveDuration := time.Duration(float64(m.moveDistance) / float64(m.speed) * float64(time.Second))
			endTime := time.Now().Add(moveDuration)

			for time.Now().Before(endTime) {
				data, err := mammotion.SendMotionControl(m.speed, 0)
				if err != nil {
					return errMsg{err}
				}
				_, err = m.cloudGateway.SendCloudCommand(m.device.IotId, data)
				if err != nil {
					return errMsg{err}
				}
				time.Sleep(200 * time.Millisecond)
			}

			// Send stop command
			stopData, err := mammotion.StopMotion()
			if err != nil {
				return errMsg{err}
			}
			_, err = m.cloudGateway.SendCloudCommand(m.device.IotId, stopData)
			if err != nil {
				return errMsg{err}
			}

			return statusMsg("✅ Movement complete")
		},
	)
}

func (m interactiveModel) moveBackward() tea.Cmd {
	return tea.Sequence(
		func() tea.Msg {
			return statusMsg("⬇️  Sending backward command...")
		},
		func() tea.Msg {
			// Refresh session to ensure identityId is valid
			if err := m.cloudGateway.CheckOrRefreshSession(); err != nil {
				return errMsg{fmt.Errorf("session refresh failed: %w", err)}
			}

			// Send move command repeatedly every 200ms
			moveDuration := time.Duration(float64(m.moveDistance) / float64(m.speed) * float64(time.Second))
			endTime := time.Now().Add(moveDuration)

			for time.Now().Before(endTime) {
				data, err := mammotion.SendMotionControl(-m.speed, 0)
				if err != nil {
					return errMsg{err}
				}
				_, err = m.cloudGateway.SendCloudCommand(m.device.IotId, data)
				if err != nil {
					return errMsg{err}
				}
				time.Sleep(200 * time.Millisecond)
			}

			// Send stop command
			stopData, err := mammotion.StopMotion()
			if err != nil {
				return errMsg{err}
			}
			_, err = m.cloudGateway.SendCloudCommand(m.device.IotId, stopData)
			if err != nil {
				return errMsg{err}
			}

			return statusMsg("✅ Movement complete")
		},
	)
}

func (m interactiveModel) turnLeft() tea.Cmd {
	return tea.Sequence(
		func() tea.Msg {
			return statusMsg("⬅️  Sending turn left command...")
		},
		func() tea.Msg {
			// Refresh session to ensure identityId is valid
			if err := m.cloudGateway.CheckOrRefreshSession(); err != nil {
				return errMsg{fmt.Errorf("session refresh failed: %w", err)}
			}

			// Send turn command repeatedly every 200ms for 3 seconds
			fmt.Printf("\nDEBUG: Turning left - linear=0, angular=-450\n")
			endTime := time.Now().Add(3 * time.Second)

			for time.Now().Before(endTime) {
				data, err := mammotion.SendMotionControl(0, -450)
				if err != nil {
					return errMsg{err}
				}
				_, err = m.cloudGateway.SendCloudCommand(m.device.IotId, data)
				if err != nil {
					return errMsg{err}
				}
				time.Sleep(200 * time.Millisecond)
			}

			// Send stop command
			stopData, err := mammotion.StopMotion()
			if err != nil {
				return errMsg{err}
			}
			_, err = m.cloudGateway.SendCloudCommand(m.device.IotId, stopData)
			if err != nil {
				return errMsg{err}
			}

			return statusMsg("✅ Turn complete")
		},
	)
}

func (m interactiveModel) turnRight() tea.Cmd {
	return tea.Sequence(
		func() tea.Msg {
			return statusMsg("➡️  Sending turn right command...")
		},
		func() tea.Msg {
			// Refresh session to ensure identityId is valid
			if err := m.cloudGateway.CheckOrRefreshSession(); err != nil {
				return errMsg{fmt.Errorf("session refresh failed: %w", err)}
			}

			// Send turn command repeatedly every 200ms for 3 seconds
			fmt.Printf("\nDEBUG: Turning right - linear=0, angular=450\n")
			endTime := time.Now().Add(3 * time.Second)

			for time.Now().Before(endTime) {
				data, err := mammotion.SendMotionControl(0, 450)
				if err != nil {
					return errMsg{err}
				}
				_, err = m.cloudGateway.SendCloudCommand(m.device.IotId, data)
				if err != nil {
					return errMsg{err}
				}
				time.Sleep(200 * time.Millisecond)
			}

			// Send stop command
			stopData, err := mammotion.StopMotion()
			if err != nil {
				return errMsg{err}
			}
			_, err = m.cloudGateway.SendCloudCommand(m.device.IotId, stopData)
			if err != nil {
				return errMsg{err}
			}

			return statusMsg("✅ Turn complete")
		},
	)
}

func (m interactiveModel) stop() tea.Cmd {
	return tea.Sequence(
		func() tea.Msg {
			return statusMsg("🛑 Sending stop command...")
		},
		func() tea.Msg {
			// Refresh session to ensure identityId is valid
			if err := m.cloudGateway.CheckOrRefreshSession(); err != nil {
				return errMsg{fmt.Errorf("session refresh failed: %w", err)}
			}

			data, err := mammotion.StopMotion()
			if err != nil {
				return errMsg{err}
			}
			_, err = m.cloudGateway.SendCloudCommand(m.device.IotId, data)
			if err != nil {
				return errMsg{err}
			}
			return statusMsg("🛑 Stopped - Ready")
		},
	)
}

func runInteractive() error {
	// Setup connection (same as battery command)
	client, err := auth.ConnectHTTP(username, password)
	if err != nil {
		return fmt.Errorf("error logging in: %w", err)
	}
	if client.LoginInfo == nil {
		return fmt.Errorf("error logging in: LoginInfo is nil")
	}

	cg := aliyuniot.NewCloudIOTGateway()
	_, err = cg.GetRegion(client.LoginInfo.UserInformation.DomainAbbreviation, client.LoginInfo.AuthorizationCode)
	if err != nil {
		return fmt.Errorf("error getting region: %w", err)
	}

	err = cg.Connect()
	if err != nil {
		return fmt.Errorf("error connecting to cloud: %w", err)
	}

	_, err = cg.LoginByOAuth(client.LoginInfo.UserInformation.DomainAbbreviation, client.LoginInfo.AuthorizationCode)
	if err != nil {
		return fmt.Errorf("IOT login error: %w", err)
	}

	err = cg.AepHandle()
	if err != nil {
		return fmt.Errorf("error handling AEP: %w", err)
	}

	err = cg.SessionByAuthCode()
	if err != nil {
		return fmt.Errorf("error getting session by auth code: %w", err)
	}

	devices, err := cg.ListDevices()
	if err != nil {
		return fmt.Errorf("error getting devices: %w", err)
	}

	if len(devices) == 0 {
		return fmt.Errorf("no devices found")
	}

	err = cg.CheckOrRefreshSession()
	if err != nil {
		return fmt.Errorf("error refreshing session: %w", err)
	}

	mqttClient := mammotion.NewMammotionMQTT(
		cg.RegionResponse.Data.RegionId,
		cg.AepResponse.Data.ProductKey,
		cg.AepResponse.Data.DeviceName,
		cg.AepResponse.Data.DeviceSecret,
		"", // Set initial token to empty, it will be set later
		cg,
	)
	mqttClient.SetIotToken(cg.SessionByAuthCodeResponse.Data.IotToken)
	mammoCloud := mammotion.NewMammotionCloud(mqttClient, cg)

	var wg sync.WaitGroup
	wg.Add(1)
	mqttReady := false
	mqttClient.OnReady = func() {
		mqttReady = true
		wg.Done()
	}
	mammoCloud.ConnectAsync()
	wg.Wait()

	// Wait a moment for MQTT to fully initialize
	if !mqttReady {
		return fmt.Errorf("MQTT connection not ready")
	}

	firstDevice := devices[0]
	mowingDevice := mammotion.NewMowingDevice(&firstDevice, *cg, mammoCloud)
	stateManager := mammotion.NewStateManager(mowingDevice)
	mammotion.NewMammotionBaseCloudDevice(mammoCloud, mowingDevice, stateManager)

	// Create channels for updates
	positionChan := make(chan position, 10)
	batteryChan := make(chan int, 10)

	// Initialize the model
	initialPos := position{x: 0, y: 0, angle: 0}
	model := interactiveModel{
		cloudGateway: cg,
		device:       &firstDevice,
		position: initialPos,
		lastPosition: initialPos,
		positionUpdates: 0,
		batteryLevel:   0,
		status:         "Connecting to device...",
		moveDistance:   1000, // 1000mm = 1 meter
		speed:          1000, // Speed units match Python example (~1000 for full speed)
		ready:          false,
		stateManager:   stateManager,
		mowingDevice:   mowingDevice,
		positionChan:   positionChan,
		batteryChan:    batteryChan,
	}

	// Set up battery updates callback
	stateManager.OnPropertiesReceived = func() {
		select {
		case batteryChan <- mowingDevice.BatteryPercentage:
		default:
			// Channel full, skip this update
		}
	}

	// Set up position updates callback
	stateManager.OnPositionUpdate = func(x, y float32, angle int32, posType int32) {
		fmt.Printf("DEBUG: OnPositionUpdate callback - X=%.0f Y=%.0f Angle=%d PosType=%d\n", x, y, angle, posType)
		select {
		case positionChan <- position{x: x, y: y, angle: angle, posType: posType}:
			fmt.Printf("DEBUG: Position sent to channel successfully\n")
		default:
			// Channel full, skip this update
			fmt.Printf("DEBUG: Position channel full, skipped update\n")
		}
	}

	// Start the TUI first
	p := tea.NewProgram(model)

	// Send initial commands in a goroutine after TUI starts
	go func() {
		// Wait a moment to ensure MQTT is fully ready
		time.Sleep(1 * time.Second)

		// Refresh session one more time to ensure identityId is set
		err := cg.CheckOrRefreshSession()
		if err != nil {
			p.Send(errMsg{err: fmt.Errorf("error refreshing session before commands: %w", err)})
			return
		}

		bleSyncData, _ := mammotion.SendTodevBleSync(3)
		_, err = cg.SendCloudCommand(firstDevice.IotId, bleSyncData)
		if err != nil {
			p.Send(errMsg{err: fmt.Errorf("error sending ble_sync: %w", err)})
			return
		}

		reportCfgData, _ := mammotion.GetReportCfg(300000, 500, 1000) // 5 min timeout, 500ms period
		_, err = cg.SendCloudCommand(firstDevice.IotId, reportCfgData)
		if err != nil {
			p.Send(errMsg{err: fmt.Errorf("error sending report_cfg: %w", err)})
			return
		}

		// Send report config periodically to keep position updates flowing
		go func() {
			ticker := time.NewTicker(60 * time.Second) // Refresh every minute
			defer ticker.Stop()
			for range ticker.C {
				cg.CheckOrRefreshSession()
				reportCfgData, _ := mammotion.GetReportCfg(300000, 500, 1000)
				cg.SendCloudCommand(firstDevice.IotId, reportCfgData)
			}
		}()

		// Mark as ready
		p.Send(readyMsg{})
	}()

	_, err = p.Run()
	if err != nil {
		return fmt.Errorf("error running TUI: %w", err)
	}

	// Cleanup
	mqttClient.Disconnect()
	close(positionChan)
	close(batteryChan)

	return nil
}
