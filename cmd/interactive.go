package cmd

import (
	"fmt"
	"sync"

	"mammo/aliyuniot"
	"mammo/auth"
	"mammo/mammotion"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type position struct {
	x     float32
	y     float32
	angle int32
}

type interactiveModel struct {
	cloudGateway   *aliyuniot.CloudIOTGateway
	device         *aliyuniot.Device
	position       position
	batteryLevel   int
	status         string
	moveDistance   int32 // movement distance in mm (300mm = 30cm)
	speed          int32 // speed in mm/s
	ready          bool
	quitting       bool
	err            error
	stateManager   *mammotion.StateManager
	mowingDevice   *mammotion.MowingDevice
	positionChan   chan position
	batteryChan    chan int
}

type positionUpdateMsg position
type batteryUpdateMsg int
type readyMsg struct{}
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
			m.moveDistance += 50
			if m.moveDistance > 1000 {
				m.moveDistance = 1000
			}

		case "-", "_":
			m.moveDistance -= 50
			if m.moveDistance < 50 {
				m.moveDistance = 50
			}
		}

	case readyMsg:
		m.ready = true
		m.status = "Connected and ready!"

	case positionUpdateMsg:
		m.position = position(msg)
		// Keep listening for more position updates
		return m, waitForPositionUpdates(m.positionChan)

	case batteryUpdateMsg:
		m.batteryLevel = int(msg)
		// Keep listening for more battery updates
		return m, waitForBatteryUpdates(m.batteryChan)

	case errMsg:
		m.err = msg.err
		return m, tea.Quit
	}

	return m, nil
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

	info := fmt.Sprintf(
		"%s\n\n"+
			"Position: X=%.2f Y=%.2f Angle=%d°\n"+
			"Battery: %d%%\n"+
			"Move Distance: %dmm (%.1fcm)\n"+
			"Speed: %dmm/s\n"+
			"Status: %s",
		status,
		m.position.x, m.position.y, m.position.angle,
		m.batteryLevel,
		m.moveDistance, float32(m.moveDistance)/10.0,
		m.speed,
		m.status,
	)

	controls := `Controls:
  ↑/W     - Move Forward
  ↓/S     - Move Backward
  ←/A     - Turn Left
  →/D     - Turn Right
  SPACE   - Stop
  +/-     - Adjust Move Distance
  Q       - Quit`

	return titleStyle.Render("🤖 Mammotion Interactive Control") + "\n" +
		boxStyle.Render(infoStyle.Render(info)) + "\n" +
		helpStyle.Render(controls)
}

// Movement commands
func (m interactiveModel) moveForward() tea.Cmd {
	return func() tea.Msg {
		m.status = "Moving forward..."
		data, err := mammotion.SendMotionControl(m.speed, 0)
		if err != nil {
			return errMsg{err}
		}
		_, err = m.cloudGateway.SendCloudCommand(m.device.IotId, data)
		if err != nil {
			return errMsg{err}
		}
		return nil
	}
}

func (m interactiveModel) moveBackward() tea.Cmd {
	return func() tea.Msg {
		m.status = "Moving backward..."
		data, err := mammotion.SendMotionControl(-m.speed, 0)
		if err != nil {
			return errMsg{err}
		}
		_, err = m.cloudGateway.SendCloudCommand(m.device.IotId, data)
		if err != nil {
			return errMsg{err}
		}
		return nil
	}
}

func (m interactiveModel) turnLeft() tea.Cmd {
	return func() tea.Msg {
		m.status = "Turning left..."
		data, err := mammotion.SendMotionControl(0, 45) // 45 degrees/s counterclockwise
		if err != nil {
			return errMsg{err}
		}
		_, err = m.cloudGateway.SendCloudCommand(m.device.IotId, data)
		if err != nil {
			return errMsg{err}
		}
		return nil
	}
}

func (m interactiveModel) turnRight() tea.Cmd {
	return func() tea.Msg {
		m.status = "Turning right..."
		data, err := mammotion.SendMotionControl(0, -45) // 45 degrees/s clockwise
		if err != nil {
			return errMsg{err}
		}
		_, err = m.cloudGateway.SendCloudCommand(m.device.IotId, data)
		if err != nil {
			return errMsg{err}
		}
		return nil
	}
}

func (m interactiveModel) stop() tea.Cmd {
	return func() tea.Msg {
		m.status = "Stopped"
		data, err := mammotion.StopMotion()
		if err != nil {
			return errMsg{err}
		}
		_, err = m.cloudGateway.SendCloudCommand(m.device.IotId, data)
		if err != nil {
			return errMsg{err}
		}
		return nil
	}
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
		cg.SessionByAuthCodeResponse.Data.IotToken,
		cg,
	)
	mammoCloud := mammotion.NewMammotionCloud(mqttClient, cg)

	var wg sync.WaitGroup
	wg.Add(1)
	mqttClient.OnReady = func() {
		wg.Done()
	}
	mammoCloud.ConnectAsync()
	wg.Wait()

	firstDevice := devices[0]
	mowingDevice := mammotion.NewMowingDevice(&firstDevice, *cg, mammoCloud)
	stateManager := mammotion.NewStateManager(mowingDevice)
	mammotion.NewMammotionBaseCloudDevice(mammoCloud, mowingDevice, stateManager)

	// Create channels for updates
	positionChan := make(chan position, 10)
	batteryChan := make(chan int, 10)

	// Initialize the model
	model := interactiveModel{
		cloudGateway: cg,
		device:       &firstDevice,
		position: position{
			x:     0,
			y:     0,
			angle: 0,
		},
		batteryLevel:   0,
		status:         "Initializing...",
		moveDistance:   300, // 30cm default
		speed:          200, // 200mm/s = 0.2m/s
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
	stateManager.OnPositionUpdate = func(x, y float32, angle int32) {
		select {
		case positionChan <- position{x: x, y: y, angle: angle}:
		default:
			// Channel full, skip this update
		}
	}

	// Send initial commands to get device reporting
	bleSyncData, _ := mammotion.SendTodevBleSync(3)
	cg.SendCloudCommand(firstDevice.IotId, bleSyncData)

	reportCfgData, _ := mammotion.GetReportCfg(10000, 1000, 2000)
	cg.SendCloudCommand(firstDevice.IotId, reportCfgData)

	// Mark as ready
	model.ready = true

	// Start the TUI
	p := tea.NewProgram(model)
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
