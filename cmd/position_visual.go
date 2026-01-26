package cmd

import (
	"encoding/base64"
	"fmt"
	"math"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"mammo/aliyuniot"
	"mammo/auth"
	"mammo/data/mqtt"
	"mammo/mammotion"
	pb "mammo/proto"

	"github.com/spf13/cobra"
	"google.golang.org/protobuf/proto"
)

// Helper functions
func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

var positionVisualCmd = &cobra.Command{
	Use:   "position-visual",
	Short: "Live ASCII visualization of mower position and path",
	Long: `Display a real-time ASCII art map showing:
  - Mower's current position and heading
  - Historical path with line drawing
  - Boundary outline of the mowing area
  - Battery level and movement speed

The visualization updates every second and automatically scales to show
both the mower's path and the boundary in the terminal window.`,
	Run: func(cmd *cobra.Command, args []string) {
		err := runPositionVisual()
		if err != nil {
			fmt.Printf("Error: %v\n", err)
		}
	},
}

type Position struct {
	x         float64
	y         float64
	timestamp time.Time
}

type VisualTracker struct {
	positions    []Position
	currentPos   *Position
	boundary     [][2]float64 // Boundary points (relative to dock)
	dockX        float64      // Dock X position (absolute)
	dockY        float64      // Dock Y position (absolute)
	minX, maxX   float64
	minY, maxY   float64
	width        int
	height       int
	battery      int32
	progress     int32
	speed        float64
	heading      float64
	mu           sync.Mutex
}

func NewVisualTracker(width, height int) *VisualTracker {
	return &VisualTracker{
		positions: make([]Position, 0, 1000),
		width:     width,
		height:    height,
	}
}

func (vt *VisualTracker) AddPosition(x, y float64, battery, progress int32, heading float64) {
	vt.mu.Lock()
	defer vt.mu.Unlock()

	pos := Position{
		x:         x,
		y:         y,
		timestamp: time.Now(),
	}

	// Update current position
	vt.currentPos = &pos

	// Calculate speed if we have previous positions
	if len(vt.positions) > 0 {
		last := vt.positions[len(vt.positions)-1]
		dx := x - last.x
		dy := y - last.y
		dist := math.Sqrt(dx*dx + dy*dy)
		deltaTime := pos.timestamp.Sub(last.timestamp).Seconds()
		if deltaTime > 0 {
			vt.speed = dist / deltaTime
		}
	}

	vt.positions = append(vt.positions, pos)
	vt.battery = battery
	vt.progress = progress
	vt.heading = heading

	// Update bounds
	if len(vt.positions) == 1 {
		vt.minX, vt.maxX = x, x
		vt.minY, vt.maxY = y, y
	} else {
		if x < vt.minX {
			vt.minX = x
		}
		if x > vt.maxX {
			vt.maxX = x
		}
		if y < vt.minY {
			vt.minY = y
		}
		if y > vt.maxY {
			vt.maxY = y
		}
	}

	// Keep only last 500 positions to prevent memory issues
	if len(vt.positions) > 500 {
		vt.positions = vt.positions[len(vt.positions)-500:]
	}
}

func (vt *VisualTracker) Draw() {
	vt.mu.Lock()
	defer vt.mu.Unlock()

	if len(vt.positions) == 0 {
		return
	}

	// Clear screen and move cursor to top
	fmt.Print("\033[H\033[2J")

	// Calculate grid size (leave room for borders and info)
	gridWidth := vt.width - 2
	gridHeight := vt.height - 8 // Leave room for header and footer

	// Ensure we have reasonable dimensions
	if gridWidth < 20 {
		gridWidth = 20
	}
	if gridHeight < 20 {
		gridHeight = 20
	}

	// Expand bounds to include boundary (in absolute coordinates)
	if len(vt.boundary) > 0 && (vt.dockX != 0 || vt.dockY != 0) {
		for _, p := range vt.boundary {
			absX := p[0] + vt.dockX
			absY := p[1] + vt.dockY
			if absX < vt.minX {
				vt.minX = absX
			}
			if absX > vt.maxX {
				vt.maxX = absX
			}
			if absY < vt.minY {
				vt.minY = absY
			}
			if absY > vt.maxY {
				vt.maxY = absY
			}
		}
	}

	// Calculate scaling
	rangeX := vt.maxX - vt.minX
	rangeY := vt.maxY - vt.minY

	// Start with a reasonable minimum view area (50m x 50m)
	minViewSize := 50.0
	if rangeX < minViewSize {
		centerX := (vt.minX + vt.maxX) / 2
		vt.minX = centerX - minViewSize/2
		vt.maxX = centerX + minViewSize/2
		rangeX = minViewSize
	}
	if rangeY < minViewSize {
		centerY := (vt.minY + vt.maxY) / 2
		vt.minY = centerY - minViewSize/2
		vt.maxY = centerY + minViewSize/2
		rangeY = minViewSize
	}

	// Add padding to bounds (10%)
	paddedMinX := vt.minX - rangeX*0.1
	paddedMaxX := vt.maxX + rangeX*0.1
	paddedMinY := vt.minY - rangeY*0.1
	paddedMaxY := vt.maxY + rangeY*0.1
	paddedRangeX := paddedMaxX - paddedMinX
	paddedRangeY := paddedMaxY - paddedMinY

	// Create grid
	grid := make([][]rune, gridHeight)
	for i := range grid {
		grid[i] = make([]rune, gridWidth)
		for j := range grid[i] {
			grid[i][j] = ' '
		}
	}

	// Plot path with line drawing between consecutive points
	for i := 0; i < len(vt.positions); i++ {
		pos := vt.positions[i]
		gridX := int((pos.x - paddedMinX) / paddedRangeX * float64(gridWidth-1))
		gridY := int((pos.y - paddedMinY) / paddedRangeY * float64(gridHeight-1))

		// Flip Y axis (so up is up on screen)
		gridY = gridHeight - 1 - gridY

		// Draw line to this point from previous point
		if i > 0 {
			prevPos := vt.positions[i-1]
			prevGridX := int((prevPos.x - paddedMinX) / paddedRangeX * float64(gridWidth-1))
			prevGridY := int((prevPos.y - paddedMinY) / paddedRangeY * float64(gridHeight-1))
			prevGridY = gridHeight - 1 - prevGridY

			// Bresenham's line algorithm
			dx := abs(gridX - prevGridX)
			dy := abs(gridY - prevGridY)
			sx := 1
			if prevGridX > gridX {
				sx = -1
			}
			sy := 1
			if prevGridY > gridY {
				sy = -1
			}
			err := dx - dy

			x, y := prevGridX, prevGridY
			for {
				if x >= 0 && x < gridWidth && y >= 0 && y < gridHeight {
					// Don't overwrite recent positions or current position
					if grid[y][x] == ' ' {
						if i >= len(vt.positions)-10 {
							grid[y][x] = '●'
						} else {
							grid[y][x] = '·'
						}
					}
				}
				if x == gridX && y == gridY {
					break
				}
				e2 := 2 * err
				if e2 > -dy {
					err -= dy
					x += sx
				}
				if e2 < dx {
					err += dx
					y += sy
				}
			}
		}

		// Mark the actual position point
		if gridX >= 0 && gridX < gridWidth && gridY >= 0 && gridY < gridHeight {
			if i == len(vt.positions)-1 {
				// Current position - use direction arrow
				arrow := getDirectionChar(vt.heading)
				grid[gridY][gridX] = arrow
			} else if i >= len(vt.positions)-10 {
				// Recent positions - use filled dot
				grid[gridY][gridX] = '●'
			} else {
				// Older positions
				grid[gridY][gridX] = '·'
			}
		}
	}

	// Draw boundary (convert from dock-relative to absolute coordinates)
	for i := 0; i < len(vt.boundary); i++ {
		p1 := vt.boundary[i]
		p2 := vt.boundary[(i+1)%len(vt.boundary)] // Wrap around to close the boundary

		// Convert from dock-relative to absolute coordinates
		absX1 := p1[0] + vt.dockX
		absY1 := p1[1] + vt.dockY
		absX2 := p2[0] + vt.dockX
		absY2 := p2[1] + vt.dockY

		gridX1 := int((absX1 - paddedMinX) / paddedRangeX * float64(gridWidth-1))
		gridY1 := int((absY1 - paddedMinY) / paddedRangeY * float64(gridHeight-1))
		gridY1 = gridHeight - 1 - gridY1

		gridX2 := int((absX2 - paddedMinX) / paddedRangeX * float64(gridWidth-1))
		gridY2 := int((absY2 - paddedMinY) / paddedRangeY * float64(gridHeight-1))
		gridY2 = gridHeight - 1 - gridY2

		// Draw line between boundary points
		dx := abs(gridX2 - gridX1)
		dy := abs(gridY2 - gridY1)
		sx := 1
		if gridX1 > gridX2 {
			sx = -1
		}
		sy := 1
		if gridY1 > gridY2 {
			sy = -1
		}
		err := dx - dy

		x, y := gridX1, gridY1
		for {
			if x >= 0 && x < gridWidth && y >= 0 && y < gridHeight {
				grid[y][x] = '#' // Boundary character
			}
			if x == gridX2 && y == gridY2 {
				break
			}
			e2 := 2 * err
			if e2 > -dy {
				err -= dy
				x += sx
			}
			if e2 < dx {
				err += dx
				y += sy
			}
		}
	}

	// Print header
	fmt.Println("╔" + strings.Repeat("═", gridWidth) + "╗")
	fmt.Printf("║ MOWER POSITION TRACKER - %d updates - Battery: %d%% ║\n",
		len(vt.positions), vt.battery)
	fmt.Println("╟" + strings.Repeat("─", gridWidth) + "╢")

	// Print grid
	for _, row := range grid {
		fmt.Print("║")
		fmt.Print(string(row))
		fmt.Println("║")
	}

	// Print footer with info
	fmt.Println("╟" + strings.Repeat("─", gridWidth) + "╢")
	if vt.currentPos != nil {
		fmt.Printf("║ Current: (%.2fm, %.2fm) │ Heading: %.1f° │ Speed: %.2fm/s │ %s ║\n",
			vt.currentPos.x, vt.currentPos.y, vt.heading, vt.speed, time.Now().Format("15:04:05"))
	}
	fmt.Printf("║ Bounds: X[%.1fm to %.1fm] Y[%.1fm to %.1fm] │ Range: %.1fm x %.1fm       ║\n",
		vt.minX, vt.maxX, vt.minY, vt.maxY, rangeX, rangeY)
	fmt.Println("╚" + strings.Repeat("═", gridWidth) + "╝")
	fmt.Println("Legend: # = boundary, ● = recent path, · = older path, ↑↗→↘↓↙←↖ = current")
}

func getDirectionChar(heading float64) rune {
	// Normalize heading to 0-360
	for heading < 0 {
		heading += 360
	}
	for heading >= 360 {
		heading -= 360
	}

	// Convert to 8 directions (compass: 0° = North/Up)
	if heading >= 337.5 || heading < 22.5 {
		return '↑' // North
	} else if heading >= 22.5 && heading < 67.5 {
		return '↗' // Northeast
	} else if heading >= 67.5 && heading < 112.5 {
		return '→' // East
	} else if heading >= 112.5 && heading < 157.5 {
		return '↘' // Southeast
	} else if heading >= 157.5 && heading < 202.5 {
		return '↓' // South
	} else if heading >= 202.5 && heading < 247.5 {
		return '↙' // Southwest
	} else if heading >= 247.5 && heading < 292.5 {
		return '←' // West
	} else {
		return '↖' // Northwest
	}
}

func getTerminalSize() (int, int) {
	cmd := exec.Command("stty", "size")
	cmd.Stdin = os.Stdin
	out, err := cmd.Output()
	if err != nil {
		// Default size if we can't detect
		return 80, 24
	}

	parts := strings.Fields(string(out))
	if len(parts) != 2 {
		return 80, 24
	}

	height, _ := strconv.Atoi(parts[0])
	width, _ := strconv.Atoi(parts[1])

	if width == 0 || height == 0 {
		return 80, 24
	}

	return width, height
}

func runPositionVisual() error {
	// Get terminal size
	width, height := getTerminalSize()

	// Setup connection
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
		"",
		cg,
	)
	mqttClient.SetIotToken(cg.SessionByAuthCodeResponse.Data.IotToken)
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

	// Create visual tracker
	tracker := NewVisualTracker(width, height)

	// Need to create base cloud device first to handle messages
	mammotion.NewMammotionBaseCloudDevice(mammoCloud, mowingDevice, stateManager)

	// Fetch boundary data
	hashListReceived := make(chan *mammotion.HashListData, 10)
	mapDataReceived := make(chan *mammotion.MapData, 100)

	stateManager.OnHashListReceived = func(hashList *mammotion.HashListData) {
		select {
		case hashListReceived <- hashList:
		default:
		}
	}

	stateManager.OnMapDataReceived = func(mapData *mammotion.MapData) {
		select {
		case mapDataReceived <- mapData:
		default:
		}
	}

	// Send initial setup commands
	bleSyncData, _ := mammotion.SendTodevBleSync(3)
	cg.SendCloudCommand(firstDevice.IotId, bleSyncData)
	time.Sleep(1 * time.Second)

	reportCfgData, _ := mammotion.GetReportCfg(10000, 1000, 2000)
	cg.SendCloudCommand(firstDevice.IotId, reportCfgData)
	time.Sleep(1 * time.Second)

	// Request hash list
	hashListData, _ := mammotion.GetHashListWithSubCmd(0)
	cg.SendCloudCommand(firstDevice.IotId, hashListData)
	time.Sleep(1 * time.Second)

	// Wait for hash list
	var boundaryHash int64
	select {
	case hashList := <-hashListReceived:
		if len(hashList.Hashes) > 0 {
			boundaryHash = hashList.Hashes[0]

			// Request boundary data
			mapDataReq, _ := mammotion.SynchronizeHashData(boundaryHash)
			cg.SendCloudCommand(firstDevice.IotId, mapDataReq)

			// Collect boundary frames with acknowledgment pattern
			allMapData := make(map[int32]*mammotion.MapData)
			timeout := time.After(15 * time.Second)
		collectLoop:
			for {
				select {
				case mapData := <-mapDataReceived:
					if mapData.Type == 0 { // Boundary type
						allMapData[mapData.CurrentFrame] = mapData

						// Check if we need more frames
						if len(allMapData) >= int(mapData.TotalFrame) {
							break collectLoop
						}

						// Send acknowledgment for this frame to request next one
						if mapData.CurrentFrame < mapData.TotalFrame {
							ackData, _ := createBoundaryFrameAck(boundaryHash, mapData.Type,
								mapData.TotalFrame, mapData.CurrentFrame)
							cg.SendCloudCommand(firstDevice.IotId, ackData)
						}
					}
				case <-timeout:
					break collectLoop
				}
			}

// Extract boundary coordinates
var boundaryPoints [][2]float64
var rawMinX, rawMaxX, rawMinY, rawMaxY int32
firstPoint := true
for frameNum := int32(1); frameNum <= int32(len(allMapData)); frameNum++ {
	if mapData, exists := allMapData[frameNum]; exists {
		for i := 0; i < len(mapData.DataCouple)-1; i += 2 {
			rawX := mapData.DataCouple[i]
			rawY := mapData.DataCouple[i+1]

			if firstPoint {
				rawMinX, rawMaxX = rawX, rawX
				rawMinY, rawMaxY = rawY, rawY
				firstPoint = false
			} else {
				if rawX < rawMinX { rawMinX = rawX }
				if rawX > rawMaxX { rawMaxX = rawX }
				if rawY < rawMinY { rawMinY = rawY }
				if rawY > rawMaxY { rawMaxY = rawY }
			}

			// Boundary coordinates are in meters, stored as integers
			x := float64(rawX)
			y := float64(rawY)
			boundaryPoints = append(boundaryPoints, [2]float64{x, y})
		}
	}
}
// Write debug to file
debugFile, _ := os.Create("/tmp/mammo_boundary_debug.txt")
if debugFile != nil {
	fmt.Fprintf(debugFile, "=== BOUNDARY DEBUG ===\n")
	fmt.Fprintf(debugFile, "Raw boundary range: X[%d to %d] Y[%d to %d]\n", rawMinX, rawMaxX, rawMinY, rawMaxY)
	fmt.Fprintf(debugFile, "Dock position: (%.2fm, %.2fm)\n", tracker.dockX, tracker.dockY)
	fmt.Fprintf(debugFile, "Boundary points: %d\n", len(boundaryPoints))
	if len(boundaryPoints) > 0 {
		fmt.Fprintf(debugFile, "First point raw: (%.0f, %.0f) -> with dock: (%.2fm, %.2fm)\n",
			boundaryPoints[0][0], boundaryPoints[0][1],
			boundaryPoints[0][0] + tracker.dockX, boundaryPoints[0][1] + tracker.dockY)
	}
	fmt.Fprintf(debugFile, "======================\n")
	debugFile.Close()
}
tracker.boundary = boundaryPoints
		}
	case <-time.After(5 * time.Second):
		// Boundary not available, continue without it
	}

	// Subscribe to messages
	mowingDevice.MqttMessageEvent().AddSubscriber(func(event interface{}) {
		thingEventMessage, ok := event.(*mqtt.ThingEventMessage)
		if !ok {
			return
		}

		var valueContent string
		switch params := thingEventMessage.Params.(type) {
		case mqtt.DeviceProtobufMsgEventParams:
			valueContent = params.Value.Content
		default:
			if generalParams, ok := thingEventMessage.Params.(mqtt.GeneralParams); ok {
				if val, ok := generalParams.Value.(map[string]interface{}); ok {
					if content, ok := val["content"].(string); ok {
						valueContent = content
					}
				}
			}
		}

		if valueContent == "" {
			return
		}

		binaryData, err := base64.StdEncoding.DecodeString(valueContent)
		if err != nil {
			return
		}

		var lubaMsg pb.LubaMsg
		err = proto.Unmarshal(binaryData, &lubaMsg)
		if err != nil {
			return
		}

		if sys := lubaMsg.GetSys(); sys != nil {
			if reportData := sys.GetToappReportData(); reportData != nil {
				var realX, realY, toward int32
				var bpPosX, bpPosY int32
				var battery, progress int32

				if locations := reportData.GetLocations(); len(locations) > 0 {
					loc := locations[0]
					realX = loc.GetRealPosX()
					realY = loc.GetRealPosY()
					toward = loc.GetRealToward()
				}

				if work := reportData.GetWork(); work != nil {
					bpPosX = work.GetBpPosX()
					bpPosY = work.GetBpPosY()
					progress = work.GetProgress()

					// Don't use BpPos for dock - it's in a different coordinate system than RealPos
					// Dock will be set from first mower position instead
				}

				if devStatus := reportData.GetDev(); devStatus != nil {
					battery = devStatus.GetBatteryVal()
				}

				// Convert to meters (using RealPos which matches boundary coordinate system)
				x := float64(realX) / 1000.0
				y := float64(realY) / 1000.0

			// Debug: Write position and dock info on first update to file
			if len(tracker.positions) == 0 {
				posDebugFile, _ := os.Create("/tmp/mammo_position_debug.txt")
				if posDebugFile != nil {
					fmt.Fprintf(posDebugFile, "=== POSITION DEBUG (first update) ===\n")
					fmt.Fprintf(posDebugFile, "RealPos raw: X=%d Y=%d (mm)\n", realX, realY)
					fmt.Fprintf(posDebugFile, "RealPos converted: (%.2fm, %.2fm)\n", x, y)
					fmt.Fprintf(posDebugFile, "BpPos raw: X=%d Y=%d (mm)\n", bpPosX, bpPosY)
					fmt.Fprintf(posDebugFile, "BpPos converted (dock): (%.2fm, %.2fm)\n",
						float64(bpPosX)/1000.0, float64(bpPosY)/1000.0)
					fmt.Fprintf(posDebugFile, "=====================================\n")
					posDebugFile.Close()
				}

				// If dock position wasn't set during boundary collection, use first mower position
				// (since mower starts on dock, RealPos should represent dock location)
				if tracker.dockX == 0 && tracker.dockY == 0 {
					tracker.dockX = x
					tracker.dockY = y
					if f, _ := os.OpenFile("/tmp/mammo_position_debug.txt", os.O_APPEND|os.O_WRONLY, 0644); f != nil {
						fmt.Fprintf(f, "Dock set to first mower pos: (%.2fm, %.2fm)\n", x, y)
						f.Close()
					}
				}
			}

				// Normalize heading
				heading := float64(toward) / 10.0
				for heading < 0 {
					heading += 360.0
				}
				for heading >= 360 {
					heading -= 360.0
				}

				// Add position to tracker
				tracker.AddPosition(x, y, battery, progress, heading)

				// Redraw
				tracker.Draw()
			}
		}
	})

	// Clear screen initially
	fmt.Print("\033[H\033[2J")

	// Start polling every 1 second
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	// Send initial request (we already sent ble_sync and report_cfg during boundary fetch)
	reportCfgPollData, _ := mammotion.GetReportCfg(10000, 1000, 1000)
	cg.SendCloudCommand(firstDevice.IotId, reportCfgPollData)

	// Poll loop
	go func() {
		for range ticker.C {
			pollData, _ := mammotion.GetReportCfg(10000, 1000, 1000)
			cg.SendCloudCommand(firstDevice.IotId, pollData)
		}
	}()

	// Keep running indefinitely
	select {}
}

// createBoundaryFrameAck creates an acknowledgment for a received frame to request the next one
func createBoundaryFrameAck(hash int64, dataType int32, totalFrame int32, currentFrame int32) ([]byte, error) {
	navGetCommData := &pb.NavGetCommData{
		Pver:         1,
		Action:       8,           // Action 8 for boundary data
		Type:         dataType,    // Type 0 for boundary
		Hash:         hash,
		TotalFrame:   totalFrame,
		CurrentFrame: currentFrame,
		SubCmd:       2, // SubCmd 2 = acknowledgment
	}

	mctlNav := &pb.MctlNav{
		SubNavMsg: &pb.MctlNav_TodevGetCommondata{
			TodevGetCommondata: navGetCommData,
		},
	}

	lubaMsg := &pb.LubaMsg{
		Msgtype:    pb.MsgCmdType_MSG_CMD_TYPE_NAV,
		Sender:     pb.MsgDevice_DEV_MOBILEAPP,
		Rcver:      pb.MsgDevice_DEV_MAINCTL,
		Msgattr:    pb.MsgAttr_MSG_ATTR_REQ,
		Seqs:       1,
		Version:    1,
		Subtype:    1,
		Timestamp:  uint64(time.Now().UnixMilli()),
		LubaSubMsg: &pb.LubaMsg_Nav{
			Nav: mctlNav,
		},
	}

	return proto.Marshal(lubaMsg)
}

func init() {
	rootCmd.AddCommand(positionVisualCmd)
}
