package cmd

import (
	"fmt"
	"log"
	"math"
	"os"
	"sync"
	"time"

	"mammo/mammotion"
	pb "mammo/proto"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

// motionController converts key presses into the continuous 200ms motion
// stream the mower expects, and guarantees a stop command when input ceases.
type motionController struct {
	mu       sync.Mutex
	linear   int32
	angular  int32
	deadline time.Time
	moving   bool
	session  *cloudSession
	notify   func(string)
}

// Drive sets the motion vector and extends the deadline. Holding a key keeps
// extending it via key-repeat; releasing lets it expire and triggers a stop.
func (mc *motionController) Drive(linear, angular int32, hold time.Duration) {
	mc.mu.Lock()
	mc.linear = linear
	mc.angular = angular
	mc.deadline = time.Now().Add(hold)
	mc.mu.Unlock()
}

// Stop cancels motion immediately.
func (mc *motionController) Stop() {
	mc.mu.Lock()
	mc.deadline = time.Time{}
	mc.mu.Unlock()
	if data, err := mammotion.StopMotion(); err == nil {
		mc.session.gateway.SendCloudCommand(mc.session.device.IotId, data)
	}
}

// run streams motion commands until the stop channel closes.
func (mc *motionController) run(stop chan struct{}) {
	ticker := time.NewTicker(150 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			mc.mu.Lock()
			wasMoving := mc.moving
			mc.mu.Unlock()
			if wasMoving {
				mc.Stop()
			}
			return
		case <-ticker.C:
			mc.mu.Lock()
			driving := time.Now().Before(mc.deadline)
			linear, angular := mc.linear, mc.angular
			wasMoving := mc.moving
			mc.moving = driving
			mc.mu.Unlock()

			if driving {
				if err := mc.session.gateway.CheckOrRefreshSession(); err != nil {
					mc.notify(fmt.Sprintf("session refresh: %v", err))
					continue
				}
				data, err := mammotion.SendMotionControl(linear, angular)
				if err != nil {
					mc.notify(fmt.Sprintf("motion build: %v", err))
					continue
				}
				if _, err := mc.session.gateway.SendCloudCommand(mc.session.device.IotId, data); err != nil {
					mc.notify(fmt.Sprintf("motion send: %v", err))
				}
			} else if wasMoving {
				if data, err := mammotion.StopMotion(); err == nil {
					mc.session.gateway.SendCloudCommand(mc.session.device.IotId, data)
					mc.notify("stopped")
				}
			}
		}
	}
}

type pilotPosMsg struct {
	x, y    float64 // meters
	heading float64 // degrees
	posType int32
}
type pilotDevStatusMsg struct {
	sysStatus   int32
	chargeState int32
}
type pilotBatteryMsg int
type pilotDockMsg DockPosition
type pilotZigZagMsg struct {
	jobID  uint64
	zone   int32
	frame  int32
	points []MapPoint
}
type pilotMapMsg *MowerMap
type pilotProgressMsg string
type pilotStatusMsg string
type pilotErrMsg struct{ err error }

type pilotModel struct {
	session  *cloudSession
	motion   *motionController
	viewOnly bool

	width, height int

	mowerMap  *MowerMap
	mapStatus string

	trail      []MapPoint
	posValid   bool
	posX, posY float64
	heading    float64
	posType    int32
	posUpdates int
	battery    int
	charging   bool

	speed      int32
	turnRate   int32
	minBattery int

	zoom       float64
	panX, panY float64
	paused     bool // last pause/resume command sent

	// Planned coverage path (zigzag) streamed during the current task.
	showPlanned bool
	plannedPath []MapPoint
	zzJobID     uint64
	zzSeen      map[int64]bool

	status   string
	err      error
	quitting bool
}

func (m pilotModel) Init() tea.Cmd { return nil }

func (m pilotModel) batteryLow() bool {
	return m.battery > 0 && m.battery < m.minBattery
}

// sendNavCmd returns a tea.Cmd that fires a one-shot nav command off the UI
// loop (refreshing the session first) and reports the outcome in the status line.
func (m pilotModel) sendNavCmd(label string, nav *pb.MctlNav) tea.Cmd {
	s := m.session
	return func() tea.Msg {
		if err := s.gateway.CheckOrRefreshSession(); err != nil {
			return pilotStatusMsg(fmt.Sprintf("%s failed: %v", label, err))
		}
		if err := sendNav(s, nav); err != nil {
			return pilotStatusMsg(fmt.Sprintf("%s failed: %v", label, err))
		}
		return pilotStatusMsg(label + " sent")
	}
}

func (m pilotModel) canDrive() bool {
	return !m.viewOnly && !m.batteryLow()
}

func (m pilotModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			m.quitting = true
			return m, tea.Quit

		case "up", "w":
			if m.canDrive() {
				m.motion.Drive(m.speed, 0, 500*time.Millisecond)
				m.status = "▲ forward"
			}
		case "down", "s":
			if m.canDrive() {
				m.motion.Drive(-m.speed, 0, 500*time.Millisecond)
				m.status = "▼ reverse"
			}
		case "left", "a":
			if m.canDrive() {
				m.motion.Drive(0, -m.turnRate, 400*time.Millisecond)
				m.status = "◀ turning left"
			}
		case "right", "d":
			if m.canDrive() {
				m.motion.Drive(0, m.turnRate, 400*time.Millisecond)
				m.status = "▶ turning right"
			}
		case " ":
			if !m.viewOnly {
				m.motion.Stop()
				m.status = "STOP sent"
			}

		case "[":
			m.speed -= 100
			if m.speed < 100 {
				m.speed = 100
			}
		case "]":
			m.speed += 100
			if m.speed > 1000 {
				m.speed = 1000
			}

		case "+", "=":
			m.zoom *= 1.5
			if m.zoom > 64 {
				m.zoom = 64
			}
		case "-", "_":
			m.zoom /= 1.5
			if m.zoom < 1 {
				m.zoom = 1
			}
		case "0":
			m.zoom = 1
			m.panX, m.panY = 0, 0
		case "h":
			m.panX -= 0.15 / m.zoom
		case "l":
			m.panX += 0.15 / m.zoom
		case "j":
			m.panY -= 0.15 / m.zoom
		case "k":
			m.panY += 0.15 / m.zoom

		case "t":
			m.showPlanned = !m.showPlanned
			switch {
			case !m.showPlanned:
				m.status = "planned path hidden"
			case len(m.plannedPath) == 0:
				m.status = "planned path shown (none received yet — needs an active task)"
			default:
				m.status = "planned path shown"
			}

		case "p":
			if !m.viewOnly {
				// NavTaskCtrl type=1: action 1 pauses the running task, 0 resumes.
				if m.paused {
					m.paused = false
					m.status = "resuming…"
					return m, m.sendNavCmd("resume", &pb.MctlNav{
						SubNavMsg: &pb.MctlNav_TodevTaskctrl{TodevTaskctrl: &pb.NavTaskCtrl{Type: 1, Action: 0}},
					})
				}
				m.paused = true
				m.status = "pausing…"
				return m, m.sendNavCmd("pause", &pb.MctlNav{
					SubNavMsg: &pb.MctlNav_TodevTaskctrl{TodevTaskctrl: &pb.NavTaskCtrl{Type: 1, Action: 1}},
				})
			}

		case "r":
			if !m.viewOnly {
				m.status = "returning to charger…"
				return m, m.sendNavCmd("return to charger", &pb.MctlNav{
					SubNavMsg: &pb.MctlNav_TodevRechgcmd{TodevRechgcmd: 1},
				})
			}
		}

	case pilotDevStatusMsg:
		m.charging = msg.chargeState == 1

	case pilotPosMsg:
		msg.heading = math.Mod(msg.heading, 360)
		if msg.heading < 0 {
			msg.heading += 360
		}
		// Position and map share one coordinate frame, so no transform is
		// needed beyond the scale applied in the callback.
		if m.posValid && len(m.trail) > 0 {
			last := m.trail[len(m.trail)-1]
			if math.Hypot(msg.x-last.X, msg.y-last.Y) > 0.02 { // 2cm jitter gate
				m.trail = append(m.trail, MapPoint{X: msg.x, Y: msg.y})
				if len(m.trail) > 4000 {
					m.trail = m.trail[len(m.trail)-4000:]
				}
			}
		} else {
			m.trail = append(m.trail, MapPoint{X: msg.x, Y: msg.y})
		}
		m.posValid = true
		m.posX, m.posY = msg.x, msg.y
		m.heading = msg.heading
		m.posType = msg.posType
		m.posUpdates++

	case pilotBatteryMsg:
		m.battery = int(msg)

	case pilotDockMsg:
		if m.mowerMap != nil {
			dock := DockPosition(msg)
			m.mowerMap.Dock = &dock
			m.status = fmt.Sprintf("dock reported by device at %.1f, %.1f", dock.X, dock.Y)
		}

	case pilotZigZagMsg:
		// A new job invalidates the previously accumulated route.
		if msg.jobID != m.zzJobID {
			m.zzJobID = msg.jobID
			m.plannedPath = m.plannedPath[:0]
			m.zzSeen = map[int64]bool{}
		}
		if m.zzSeen == nil {
			m.zzSeen = map[int64]bool{}
		}
		key := int64(msg.zone)<<32 | int64(uint32(msg.frame))
		if !m.zzSeen[key] {
			m.zzSeen[key] = true
			m.plannedPath = append(m.plannedPath, msg.points...)
		}

	case pilotMapMsg:
		m.mowerMap = (*MowerMap)(msg)
		m.mapStatus = ""

	case pilotProgressMsg:
		m.mapStatus = string(msg)

	case pilotStatusMsg:
		m.status = string(msg)

	case pilotErrMsg:
		m.err = msg.err
		return m, tea.Quit
	}

	return m, nil
}

func rtkLabel(posType int32) string {
	switch posType {
	case 4:
		return "RTK-Fix"
	case 5:
		return "RTK-Float"
	case 1:
		return "DGPS"
	case 0:
		return "GPS"
	default:
		return fmt.Sprintf("fix?%d", posType)
	}
}

var (
	pilotHeaderStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("48"))
	pilotWarnStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("196"))
	pilotHelpStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
)

func (m pilotModel) View() string {
	if m.quitting {
		return "Stopping mower and disconnecting...\n"
	}
	if m.err != nil {
		return fmt.Sprintf("Error: %v\n", m.err)
	}
	if m.width == 0 {
		return "starting..."
	}

	mapHeight := m.height - 3
	if mapHeight < 5 {
		mapHeight = 5
	}
	canvas := NewCanvas(m.width, mapHeight)

	// Determine world bounds: map extent plus mower trail.
	var minX, minY, maxX, maxY float64
	haveBounds := false
	if m.mowerMap != nil {
		minX, minY, maxX, maxY, haveBounds = m.mowerMap.Bounds()
	}
	if m.posValid {
		if !haveBounds {
			minX, minY, maxX, maxY = m.posX-10, m.posY-10, m.posX+10, m.posY+10
			haveBounds = true
		} else {
			minX = math.Min(minX, m.posX)
			maxX = math.Max(maxX, m.posX)
			minY = math.Min(minY, m.posY)
			maxY = math.Max(maxY, m.posY)
		}
	}

	offScreen := false
	offDist := 0.0
	if haveBounds {
		vp := NewViewport(minX, minY, maxX, maxY, canvas.PixelW(), canvas.PixelH(), 0.05)
		if m.zoom != 1 {
			vp.Zoom(m.zoom)
		}
		if m.panX != 0 || m.panY != 0 {
			vp.Pan(m.panX, m.panY)
		}
		if m.mowerMap != nil {
			DrawMap(canvas, vp, m.mowerMap, nil)
		}
		// Planned coverage route under the live trail, so the trail shows
		// progress over the plan.
		if m.showPlanned && len(m.plannedPath) > 1 {
			DrawPolyline(canvas, vp, m.plannedPath, colPlanned)
		}
		DrawTrail(canvas, vp, m.trail)
		if m.posValid {
			px, py := vp.ToPixel(m.posX, m.posY)
			if px >= 0 && px < canvas.PixelW() && py >= 0 && py < canvas.PixelH() {
				DrawMower(canvas, vp, m.posX, m.posY, m.heading)
			} else {
				offScreen = DrawOffScreenMarker(canvas, vp, m.posX, m.posY, colMower)
				cx := (vp.MinX + vp.MaxX) / 2
				cy := (vp.MinY + vp.MaxY) / 2
				offDist = math.Hypot(m.posX-cx, m.posY-cy)
			}
		}
	}
	if m.mapStatus != "" {
		canvas.OverlayString(1, 0, m.mapStatus, colLabel)
	}

	mapLines := canvas.Render()
	body := ""
	for _, l := range mapLines {
		body += l + "\n"
	}

	// Header
	mode := "PILOT"
	if m.viewOnly {
		mode = "VIEW"
	}
	bat := fmt.Sprintf("bat %d%%", m.battery)
	if m.charging {
		bat += "⚡"
	}
	frame := ""
	if m.paused {
		frame += " │ PAUSED"
	}
	if len(m.plannedPath) > 1 {
		if m.showPlanned {
			frame += " │ plan shown"
		} else {
			frame += " │ plan hidden"
		}
	}
	if offScreen {
		frame += fmt.Sprintf(" │ mower off-screen %.0fm (arrow; press 0 to fit)", offDist)
	}
	header := fmt.Sprintf(" %s │ %s │ %s │ pos %.2f, %.2f │ hdg %.0f° │ spd %d │ zoom %.1fx%s │ %s",
		mode, bat, rtkLabel(m.posType), m.posX, m.posY, m.heading, m.speed, m.zoom, frame, m.status)
	headerLine := pilotHeaderStyle.Render(header)
	if m.batteryLow() && !m.viewOnly {
		headerLine = pilotWarnStyle.Render(fmt.Sprintf(" ⚠ BATTERY %d%% — driving disabled (below --min-battery %d%%) ",
			m.battery, m.minBattery)) + headerLine
	}

	help := " wasd/arrows drive · space STOP · p pause · r dock · t plan · [ ] speed · +- zoom · hjkl pan · 0 fit · q quit"
	if m.viewOnly {
		help = " t plan · + - zoom · hjkl pan · 0 fit · q quit"
	}

	return headerLine + "\n" + body + pilotHelpStyle.Render(help)
}

var (
	pilotMapFile    string
	pilotSaveMap    string
	pilotViewOnly   bool
	pilotMinBattery int
)

var pilotCmd = &cobra.Command{
	Use:     "pilot",
	Aliases: []string{"interactive", "position-visual"},
	Short:   "Drive the mower on a live high-resolution map",
	Long: `Full-screen TUI combining the live map and interactive driving.

The mower's map (areas, obstacles, paths, dock) renders in braille
sub-character resolution with the mower's position, heading and trail
drawn live on top. Driving uses the same continuous motion protocol as
the Mammotion app.

The planned coverage route for the current task (the mower's zigzag
path) is overlaid in cyan as the device streams it; toggle it with t.

Controls:
  wasd / arrows  drive          space  emergency stop
  p              pause / resume  r      return to charger
  t              toggle planned coverage path
  [ ]            drive speed     + -    zoom      hjkl  pan
  0              fit view        q      quit

Driving is disabled below --min-battery (default 15%) so a low battery
can't be run flat away from the dock. Pause and return-to-charger remain
available at any battery level.`,
	Run: func(cmd *cobra.Command, args []string) {
		// The mammotion package logs diagnostics to stderr, which corrupts a
		// full-screen TUI. Divert them to a file for the duration.
		if logFile, err := os.OpenFile("/tmp/mammo-pilot.log", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644); err == nil {
			log.SetOutput(logFile)
			defer func() {
				log.SetOutput(os.Stderr)
				logFile.Close()
			}()
		}
		withSession(func(s *cloudSession) error {
			stopPolling := startPolling(s)
			defer stopPolling()

			motion := &motionController{session: s, notify: func(string) {}}

			model := pilotModel{
				session:     s,
				motion:      motion,
				viewOnly:    pilotViewOnly,
				speed:       400,
				turnRate:    450,
				minBattery:  pilotMinBattery,
				zoom:        1,
				showPlanned: true,
				status:      "connected",
				mapStatus:   "fetching map from mower...",
			}

			p := tea.NewProgram(model, tea.WithAltScreen())
			motion.notify = func(msg string) { p.Send(pilotStatusMsg(msg)) }

			s.stateManager.OnPositionUpdate = func(x, y float32, angle int32, posType int32) {
				// RealPos is in 0.1mm units (÷10000 → metres) and real_toward
				// is in 0.0001° units (÷10000 → degrees), both in the same
				// frame as the stored map. Heading is already a compass bearing
				// (0=north, clockwise, ±180), verified against movement
				// direction; the model normalises it to 0-360.
				p.Send(pilotPosMsg{
					x:       float64(x) / 10000.0,
					y:       float64(y) / 10000.0,
					heading: float64(angle) / 10000.0,
					posType: posType,
				})
			}
			s.stateManager.OnPropertiesReceived = func() {
				p.Send(pilotBatteryMsg(s.mowingDevice.BatteryPercentage))
			}
			s.stateManager.OnDeviceStatus = func(sysStatus, chargeState int32) {
				p.Send(pilotDevStatusMsg{sysStatus: sysStatus, chargeState: chargeState})
			}
			s.stateManager.OnZigZagReceived = func(zz *mammotion.ZigZagData) {
				// Page to the next frame so we collect the whole route.
				if zz.CurrentFrame < zz.TotalFrame {
					if ack, err := buildZigZagAck(zz.CurrentZone, zz.CurrentHash, zz.TotalFrame, zz.CurrentFrame); err == nil {
						s.gateway.SendCloudCommand(s.device.IotId, ack)
					}
				}
				pts := make([]MapPoint, 0, len(zz.DataCouple)/2)
				for i := 0; i+1 < len(zz.DataCouple); i += 2 {
					pts = append(pts, MapPoint{X: float64(zz.DataCouple[i]), Y: float64(zz.DataCouple[i+1])})
				}
				p.Send(pilotZigZagMsg{jobID: zz.JobId, zone: zz.CurrentZone, frame: zz.CurrentFrame, points: pts})
			}

			// Map: load from file or fetch live in the background.
			go func() {
				if pilotMapFile != "" {
					m, err := LoadMap(pilotMapFile)
					if err != nil {
						p.Send(pilotProgressMsg(fmt.Sprintf("map load failed: %v", err)))
						return
					}
					p.Send(pilotMapMsg(m))
					return
				}
				m, err := FetchMap(s, func(msg string) { p.Send(pilotProgressMsg(msg)) })
				if err != nil {
					p.Send(pilotProgressMsg(fmt.Sprintf("map fetch failed: %v", err)))
					return
				}
				// FetchMap clears its callbacks on exit; re-register the dock
				// listener so a late toapp_chgpileto still reaches us.
				s.stateManager.OnChargePilePosition = func(toward int32, x, y float32) {
					p.Send(pilotDockMsg(DockPosition{X: float64(x), Y: float64(y), Toward: toward}))
				}
				p.Send(pilotMapMsg(m))
				if pilotSaveMap != "" {
					if err := SaveMap(m, pilotSaveMap); err != nil {
						p.Send(pilotStatusMsg(fmt.Sprintf("map save failed: %v", err)))
					} else {
						p.Send(pilotStatusMsg("map saved to " + pilotSaveMap))
					}
				}
			}()

			motionStop := make(chan struct{})
			if !pilotViewOnly {
				go motion.run(motionStop)
			}

			_, err := p.Run()
			close(motionStop)
			if !pilotViewOnly {
				motion.Stop() // belt and braces: never leave the mower driving
			}
			return err
		})
	},
}

func init() {
	pilotCmd.Flags().StringVar(&pilotMapFile, "map", "", "render a saved map file instead of fetching from the mower")
	pilotCmd.Flags().StringVar(&pilotSaveMap, "save-map", "", "save the fetched map to a JSON file")
	pilotCmd.Flags().BoolVar(&pilotViewOnly, "view-only", false, "disable driving controls")
	pilotCmd.Flags().IntVar(&pilotMinBattery, "min-battery", 15, "disable driving below this battery percentage")
	rootCmd.AddCommand(pilotCmd)
}
