package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"mammo/mammotion"
	pb "mammo/proto"

	"github.com/spf13/cobra"
	"google.golang.org/protobuf/proto"
)

// MapPoint is a world coordinate in meters.
type MapPoint struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// DockPosition is the charge pile location and orientation.
type DockPosition struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Toward int32   `json:"toward"`
}

// MapElement is one geometric feature of the mower's map.
type MapElement struct {
	Hash     int64      `json:"hash"`
	Type     int32      `json:"type"`
	TypeName string     `json:"typeName"`
	Label    string     `json:"label,omitempty"`
	Points   []MapPoint `json:"points"`
}

// MowerMap is the local (downloadable/uploadable) map file format.
type MowerMap struct {
	FormatVersion int           `json:"formatVersion"`
	Device        string        `json:"device"`
	DeviceIotId   string        `json:"deviceIotId,omitempty"`
	DownloadedAt  time.Time     `json:"downloadedAt"`
	Dock          *DockPosition `json:"dock,omitempty"`
	Elements      []MapElement  `json:"elements"`
}

func mapTypeName(t int32) string {
	switch t {
	case 0:
		return "area"
	case 1:
		return "obstacle"
	case 2:
		return "path"
	case 12:
		return "dump-point"
	case 13:
		return "svg"
	default:
		return fmt.Sprintf("type-%d", t)
	}
}

// Bounds returns the world-coordinate extent of all map content.
func (m *MowerMap) Bounds() (minX, minY, maxX, maxY float64, ok bool) {
	first := true
	add := func(x, y float64) {
		if first {
			minX, maxX, minY, maxY = x, x, y, y
			first = false
			return
		}
		if x < minX {
			minX = x
		}
		if x > maxX {
			maxX = x
		}
		if y < minY {
			minY = y
		}
		if y > maxY {
			maxY = y
		}
	}
	for _, el := range m.Elements {
		for _, p := range el.Points {
			add(p.X, p.Y)
		}
	}
	if m.Dock != nil {
		add(m.Dock.X, m.Dock.Y)
	}
	return minX, minY, maxX, maxY, !first
}

// DockEstimate returns the best-known dock location in map coordinates.
// Preference: explicit dock (toapp_chgpileto); else the map's single-point
// path element — boundary recording starts at the dock, and the charge point
// is stored as a lone path vertex; else the map origin.
func (m *MowerMap) DockEstimate() (MapPoint, string) {
	if m.Dock != nil {
		return MapPoint{X: m.Dock.X, Y: m.Dock.Y}, "device"
	}
	for _, el := range m.Elements {
		if el.Type == 2 && len(el.Points) == 1 {
			return el.Points[0], "charge point"
		}
	}
	return MapPoint{}, "unknown (map origin)"
}

// PointCount is the total number of vertices across all elements.
func (m *MowerMap) PointCount() int {
	n := 0
	for _, el := range m.Elements {
		n += len(el.Points)
	}
	return n
}

func SaveMap(m *MowerMap, path string) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func LoadMap(path string) (*MowerMap, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m MowerMap
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return &m, nil
}

// buildFrameAck acknowledges a received map frame so the device sends the
// next one (NavGetCommData with SubCmd=2).
func buildFrameAck(hash int64, dataType, totalFrame, currentFrame int32) ([]byte, error) {
	return proto.Marshal(&pb.LubaMsg{
		Msgtype:   pb.MsgCmdType_MSG_CMD_TYPE_NAV,
		Sender:    pb.MsgDevice_DEV_MOBILEAPP,
		Rcver:     pb.MsgDevice_DEV_MAINCTL,
		Msgattr:   pb.MsgAttr_MSG_ATTR_REQ,
		Seqs:      1,
		Version:   1,
		Subtype:   1,
		Timestamp: uint64(time.Now().UnixMilli()),
		LubaSubMsg: &pb.LubaMsg_Nav{Nav: &pb.MctlNav{
			SubNavMsg: &pb.MctlNav_TodevGetCommondata{TodevGetCommondata: &pb.NavGetCommData{
				Pver:         1,
				Action:       8,
				Type:         dataType,
				Hash:         hash,
				TotalFrame:   totalFrame,
				CurrentFrame: currentFrame,
				SubCmd:       2,
			}},
		}},
	})
}

// buildZigZagAck acknowledges a received coverage-path frame so the device
// sends the next one (NavUploadZigZagResultAck, mirroring the map-data ack).
func buildZigZagAck(zone int32, hash uint64, totalFrame, currentFrame int32) ([]byte, error) {
	return proto.Marshal(&pb.LubaMsg{
		Msgtype:   pb.MsgCmdType_MSG_CMD_TYPE_NAV,
		Sender:    pb.MsgDevice_DEV_MOBILEAPP,
		Rcver:     pb.MsgDevice_DEV_MAINCTL,
		Msgattr:   pb.MsgAttr_MSG_ATTR_REQ,
		Seqs:      1,
		Version:   1,
		Subtype:   1,
		Timestamp: uint64(time.Now().UnixMilli()),
		LubaSubMsg: &pb.LubaMsg_Nav{Nav: &pb.MctlNav{
			SubNavMsg: &pb.MctlNav_TodevZigzagAck{TodevZigzagAck: &pb.NavUploadZigZagResultAck{
				Pver:         1,
				CurrentZone:  zone,
				CurrentHash:  hash,
				TotalFrame:   totalFrame,
				CurrentFrame: currentFrame,
				SubCmd:       1,
			}},
		}},
	})
}

// FetchMap downloads the full map (all hashes, all frames) from the mower.
// Progress messages go through report (may be nil). Read-only operation.
func FetchMap(s *cloudSession, report func(string)) (*MowerMap, error) {
	if report == nil {
		report = func(string) {}
	}

	hashCh := make(chan *mammotion.HashListData, 4)
	mapCh := make(chan *mammotion.MapData, 256)
	dockCh := make(chan DockPosition, 4)

	s.stateManager.OnHashListReceived = func(h *mammotion.HashListData) {
		select {
		case hashCh <- h:
		default:
		}
	}
	s.stateManager.OnMapDataReceived = func(md *mammotion.MapData) {
		select {
		case mapCh <- md:
		default:
		}
	}
	s.stateManager.OnChargePilePosition = func(toward int32, x, y float32) {
		select {
		case dockCh <- DockPosition{X: float64(x), Y: float64(y), Toward: toward}:
		default:
		}
	}
	defer func() {
		s.stateManager.OnHashListReceived = nil
		s.stateManager.OnMapDataReceived = nil
		s.stateManager.OnChargePilePosition = nil
	}()

	// Request the hash list.
	report("Requesting map hash list...")
	hashListData, err := mammotion.GetHashListWithSubCmd(0)
	if err != nil {
		return nil, err
	}
	if _, err := s.gateway.SendCloudCommand(s.device.IotId, hashListData); err != nil {
		return nil, fmt.Errorf("request hash list: %w", err)
	}

	var hashes []int64
	select {
	case hl := <-hashCh:
		hashes = hl.Hashes
	case <-time.After(10 * time.Second):
		return nil, fmt.Errorf("timed out waiting for hash list (is the mower online?)")
	}
	report(fmt.Sprintf("Got %d map element(s) to fetch", len(hashes)))

	m := &MowerMap{
		FormatVersion: 1,
		Device:        s.device.DeviceName,
		DeviceIotId:   s.device.IotId,
		DownloadedAt:  time.Now(),
	}

	for i, hash := range hashes {
		el, err := fetchElement(s, hash, mapCh, report)
		if err != nil {
			report(fmt.Sprintf("  element %d/%d (hash %d): %v — skipping", i+1, len(hashes), hash, err))
			continue
		}
		m.Elements = append(m.Elements, *el)
		report(fmt.Sprintf("  element %d/%d: %s %q — %d points",
			i+1, len(hashes), el.TypeName, el.Label, len(el.Points)))
	}

	// Dock position may have arrived at any point during the session.
	select {
	case dock := <-dockCh:
		m.Dock = &dock
	default:
	}

	if len(m.Elements) == 0 {
		return m, fmt.Errorf("no map elements could be fetched")
	}
	return m, nil
}

// fetchElement requests all frames for one hash and assembles the element.
func fetchElement(s *cloudSession, hash int64, mapCh chan *mammotion.MapData, report func(string)) (*MapElement, error) {
	// Drain stale frames from previous elements.
	for {
		select {
		case <-mapCh:
			continue
		default:
		}
		break
	}

	reqData, err := mammotion.SynchronizeHashData(hash)
	if err != nil {
		return nil, err
	}
	if _, err := s.gateway.SendCloudCommand(s.device.IotId, reqData); err != nil {
		return nil, fmt.Errorf("request data: %w", err)
	}

	frames := make(map[int32]*mammotion.MapData)
	var elType int32 = -1
	var label string
	var totalFrame int32 = 1
	timeout := time.After(20 * time.Second)

	for {
		select {
		case md := <-mapCh:
			if md.Hash != hash {
				continue // frame for a different element
			}
			frames[md.CurrentFrame] = md
			elType = md.Type
			totalFrame = md.TotalFrame
			if md.AreaLabel != "" {
				label = md.AreaLabel
			}
			if int32(len(frames)) >= totalFrame {
				goto assembled
			}
			// Ack this frame to request the next.
			if md.CurrentFrame < totalFrame {
				if ack, err := buildFrameAck(hash, md.Type, md.TotalFrame, md.CurrentFrame); err == nil {
					s.gateway.SendCloudCommand(s.device.IotId, ack)
				}
			}
		case <-timeout:
			if len(frames) == 0 {
				return nil, fmt.Errorf("no frames received")
			}
			report(fmt.Sprintf("  hash %d: timeout with %d/%d frames, keeping partial data", hash, len(frames), totalFrame))
			goto assembled
		}
	}

assembled:
	el := &MapElement{
		Hash:     hash,
		Type:     elType,
		TypeName: mapTypeName(elType),
		Label:    label,
	}
	for f := int32(1); f <= totalFrame; f++ {
		md, ok := frames[f]
		if !ok {
			continue
		}
		for i := 0; i+1 < len(md.DataCouple); i += 2 {
			el.Points = append(el.Points, MapPoint{
				X: float64(md.DataCouple[i]),
				Y: float64(md.DataCouple[i+1]),
			})
		}
	}
	return el, nil
}

// renderMapSnapshot draws a one-shot view of a map (plus optional mower
// position) sized to the terminal, and returns the lines to print.
func renderMapSnapshot(m *MowerMap, width, height int) []string {
	canvas := NewCanvas(width, height)
	minX, minY, maxX, maxY, ok := m.Bounds()
	if !ok {
		return []string{"(map has no points)"}
	}
	vp := NewViewport(minX, minY, maxX, maxY, canvas.PixelW(), canvas.PixelH(), 0.05)
	DrawMap(canvas, vp, m, nil)
	return canvas.Render()
}

func terminalSize() (int, int) {
	cmd := exec.Command("stty", "size")
	cmd.Stdin = os.Stdin
	out, err := cmd.Output()
	if err != nil {
		return 100, 40
	}
	parts := strings.Fields(string(out))
	if len(parts) != 2 {
		return 100, 40
	}
	height, _ := strconv.Atoi(parts[0])
	width, _ := strconv.Atoi(parts[1])
	if width <= 0 || height <= 0 {
		return 100, 40
	}
	return width, height
}

var mapDownloadOutput string

var mapDownloadCmd = &cobra.Command{
	Use:   "map-download",
	Short: "Download the mower's map (areas, obstacles, paths, dock) to a JSON file",
	Run: func(cmd *cobra.Command, args []string) {
		withSession(func(s *cloudSession) error {
			out := mapDownloadOutput
			if out == "" {
				out = fmt.Sprintf("map-%s.json", time.Now().Format("2006-01-02-150405"))
			}
			m, err := FetchMap(s, func(msg string) { fmt.Println(msg) })
			if err != nil {
				return err
			}
			if err := SaveMap(m, out); err != nil {
				return err
			}
			minX, minY, maxX, maxY, _ := m.Bounds()
			fmt.Printf("Saved %d element(s), %d points to %s\n", len(m.Elements), m.PointCount(), out)
			fmt.Printf("Extent: %.1fm x %.1fm  (battery: %d%%)\n", maxX-minX, maxY-minY, s.mowingDevice.BatteryPercentage)
			return nil
		})
	},
}

var mapShowCmd = &cobra.Command{
	Use:   "map-show <map.json>",
	Short: "Render a downloaded map file in the terminal (offline, high resolution)",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		m, err := LoadMap(args[0])
		if err != nil {
			fmt.Println("Error:", err)
			os.Exit(1)
		}
		width, height := terminalSize()
		for _, line := range renderMapSnapshot(m, width, height-4) {
			fmt.Println(line)
		}
		minX, minY, maxX, maxY, _ := m.Bounds()
		fmt.Printf("%s — %d elements, %d points, %.1fm x %.1fm\n",
			m.Device, len(m.Elements), m.PointCount(), maxX-minX, maxY-minY)
		fmt.Println("Legend: green=area red=obstacle gold=path ⌂=dock")
	},
}

var mapUploadCmd = &cobra.Command{
	Use:   "map-upload <map.json>",
	Short: "Upload a saved map to the mower (NOT YET SUPPORTED by the known protocol)",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		m, err := LoadMap(args[0])
		if err != nil {
			fmt.Println("Error:", err)
			os.Exit(1)
		}
		fmt.Printf("Map file is valid: %d element(s), %d points, device %q.\n",
			len(m.Elements), m.PointCount(), m.Device)
		fmt.Println()
		fmt.Println("Uploading map geometry to the mower is not implemented: the known")
		fmt.Println("Mammotion protobuf protocol has no app→device write for map data.")
		fmt.Println("The device-bound nav messages (NavGetCommData, NavPlanJobSet,")
		fmt.Println("NavReqCoverPath) only request data or reference existing zone hashes;")
		fmt.Println("none carry coordinate payloads toward the device. Maps are created")
		fmt.Println("on-device by boundary recording, and sending speculative writes")
		fmt.Println("risks corrupting the stored map.")
		fmt.Println()
		fmt.Println("This command validates map files so they're ready if a safe upload")
		fmt.Println("path is found (e.g. via app traffic capture).")
		os.Exit(2)
	},
}

func init() {
	mapDownloadCmd.Flags().StringVarP(&mapDownloadOutput, "output", "o", "", "output file (default map-<timestamp>.json)")
	rootCmd.AddCommand(mapDownloadCmd)
	rootCmd.AddCommand(mapShowCmd)
	rootCmd.AddCommand(mapUploadCmd)
}
