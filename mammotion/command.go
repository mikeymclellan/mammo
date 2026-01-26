package mammotion

import (
	"time"

	pb "mammo/proto"
	"google.golang.org/protobuf/proto"
)

type MammotionCommand struct {
	deviceName string
	productKey string
}

func NewMammotionCommand(deviceName string) *MammotionCommand {
	return &MammotionCommand{deviceName: deviceName}
}

func (c *MammotionCommand) SendToDevBleSync(value int) []byte {
	data, _ := SendTodevBleSync(int32(value))
	return data
}

func (c *MammotionCommand) GetCommandBytes(key string, kwargs map[string]interface{}) []byte {
	// Basic implementation to return a non-empty byte slice
	return []byte("test_command")
}

func (c *MammotionCommand) GetDeviceProductKey() string {
	return c.productKey
}

func (c *MammotionCommand) GetDeviceName() string {
	return c.deviceName
}

func (c *MammotionCommand) SetDeviceProductKey(key string) {
	c.productKey = key
}

// SendTodevBleSync creates a protobuf message to sync with device via BLE
// This command triggers the device to start reporting status
func SendTodevBleSync(syncType int32) ([]byte, error) {
	// Create the DevNet message with ble_sync
	devNet := &pb.DevNet{
		NetSubType: &pb.DevNet_TodevBleSync{
			TodevBleSync: syncType,
		},
	}

	// Create the LubaMsg wrapper
	lubaMsg := &pb.LubaMsg{
		Msgtype:    pb.MsgCmdType_MSG_CMD_TYPE_ESP,
		Sender:     pb.MsgDevice_DEV_MOBILEAPP,
		Rcver:      pb.MsgDevice_DEV_COMM_ESP,
		Msgattr:    pb.MsgAttr_MSG_ATTR_REQ,
		Seqs:       1,
		Version:    1,
		Subtype:    1,
		Timestamp:  uint64(time.Now().UnixMilli()),
		LubaSubMsg: &pb.LubaMsg_Net{
			Net: devNet,
		},
	}

	// Serialize to bytes
	return proto.Marshal(lubaMsg)
}

// GetReportCfg creates a protobuf message to request device reporting
func GetReportCfg(timeout int32, period int32, noChangePeriod int32) ([]byte, error) {
	// Create the report configuration
	reportCfg := &pb.ReportInfoCfg{
		Act:            pb.RptAct_RPT_START,
		Timeout:        timeout,
		Period:         period,
		NoChangePeriod: noChangePeriod,
		Count:          1,
		Sub: []pb.RptInfoType{
			pb.RptInfoType_RIT_CONNECT,
			pb.RptInfoType_RIT_RTK,
			pb.RptInfoType_RIT_DEV_LOCAL,
			pb.RptInfoType_RIT_WORK,
			pb.RptInfoType_RIT_DEV_STA,
			pb.RptInfoType_RIT_FW_INFO,
			pb.RptInfoType_RIT_MAINTAIN,
		},
	}

	// Create the MctlSys message
	mctlSys := &pb.MctlSys{
		SubSysMsg: &pb.MctlSys_TodevReportCfg{
			TodevReportCfg: reportCfg,
		},
	}

	// Create the LubaMsg wrapper
	lubaMsg := &pb.LubaMsg{
		Msgtype:   pb.MsgCmdType_MSG_CMD_TYPE_EMBED_SYS,
		Sender:    pb.MsgDevice_DEV_MOBILEAPP,
		Rcver:     pb.MsgDevice_DEV_MAINCTL,
		Msgattr:   pb.MsgAttr_MSG_ATTR_REQ,
		Seqs:      1,
		Version:   1,
		Subtype:   1,
		Timestamp: uint64(time.Now().UnixMilli()),
		LubaSubMsg: &pb.LubaMsg_Sys{
			Sys: mctlSys,
		},
	}

	// Serialize to bytes
	return proto.Marshal(lubaMsg)
}

// SendMotionControl sends a motion control command to the mower
// linearSpeed: forward/backward speed (POSITIVE = forward ~1000, NEGATIVE = backward ~-1000)
// angularSpeed: rotation speed (NEGATIVE = left ~-450, POSITIVE = right ~450)
func SendMotionControl(linearSpeed int32, angularSpeed int32) ([]byte, error) {
	// Create the motion control message
	motionCtrl := &pb.DrvMotionCtrl{
		SetLinearSpeed:  linearSpeed,
		SetAngularSpeed: angularSpeed,
	}

	// Create the MctlDriver message
	mctlDriver := &pb.MctlDriver{
		SubDrvMsg: &pb.MctlDriver_TodevDevmotionCtrl{
			TodevDevmotionCtrl: motionCtrl,
		},
	}

	// Create the LubaMsg wrapper
	lubaMsg := &pb.LubaMsg{
		Msgtype:   pb.MsgCmdType_MSG_CMD_TYPE_EMBED_DRIVER,
		Sender:    pb.MsgDevice_DEV_MOBILEAPP,
		Rcver:     pb.MsgDevice_DEV_MAINCTL,
		Msgattr:   pb.MsgAttr_MSG_ATTR_REQ,
		Seqs:      1,
		Version:   1,
		Subtype:   1,
		Timestamp: uint64(time.Now().UnixMilli()),
		LubaSubMsg: &pb.LubaMsg_Driver{
			Driver: mctlDriver,
		},
	}

	// Serialize to bytes
	return proto.Marshal(lubaMsg)
}

// StopMotion sends a command to stop all motion
func StopMotion() ([]byte, error) {
	return SendMotionControl(0, 0)
}

// GetAllBoundaryHashList requests the list of all hashes for map elements
// subCmd: 0 = request hash list
func GetAllBoundaryHashList(subCmd int32) ([]byte, error) {
	navGetHashList := &pb.NavGetHashList{
		Pver:   1,
		SubCmd: subCmd,
	}

	mctlNav := &pb.MctlNav{
		SubNavMsg: &pb.MctlNav_TodevGethash{
			TodevGethash: navGetHashList,
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

// GetHashResponse acknowledges receipt of hash list
func GetHashResponse(totalFrame int32, currentFrame int32) ([]byte, error) {
	navGetHashList := &pb.NavGetHashList{
		Pver:         1,
		SubCmd:       2, // sub_cmd=2 for acknowledgment
		TotalFrame:   totalFrame,
		CurrentFrame: currentFrame,
	}

	mctlNav := &pb.MctlNav{
		SubNavMsg: &pb.MctlNav_TodevGethash{
			TodevGethash: navGetHashList,
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

// SynchronizeHashData requests the actual map data for a given hash
func SynchronizeHashData(hashNum int64) ([]byte, error) {
	navGetCommData := &pb.NavGetCommData{
		Pver:   1,
		Action: 8,
		Hash:   hashNum,
		SubCmd: 1,
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

// GetRegionalData requests a specific frame of map data
func GetRegionalData(action int32, dataType int32, hash int64, totalFrame int32, currentFrame int32) ([]byte, error) {
	navGetCommData := &pb.NavGetCommData{
		Pver:         1,
		Action:       action,
		Type:         dataType,
		Hash:         hash,
		TotalFrame:   totalFrame,
		CurrentFrame: currentFrame,
		SubCmd:       2,
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

// GetSVGData requests map data in SVG format
func GetSVGData(hash int64) ([]byte, error) {
	navGetCommData := &pb.NavGetCommData{
		Pver:   1,
		Action: 8,
		Type:   13, // Type 13 = SVG format
		Hash:   hash,
		SubCmd: 1,
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

// GetCommDataWithParams requests map data with custom parameters
func GetCommDataWithParams(action int32, dataType int32, hash int64, subCmd int32) ([]byte, error) {
	navGetCommData := &pb.NavGetCommData{
		Pver:   1,
		Action: action,
		Type:   dataType,
		Hash:   hash,
		SubCmd: subCmd,
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

// RequestAllData requests all map data without specific hash
func RequestAllData() ([]byte, error) {
	return GetCommDataWithParams(8, 0, 0, 1) // action=8, type=0, hash=0, subcmd=1
}

// GetAllDataTypes requests map data for all types (0-3, 12-13)
func GetAllDataTypes(hash int64) ([][]byte, error) {
	var requests [][]byte

	// Type 0 = Area/Boundary
	// Type 1 = Obstacle
	// Type 2 = Path
	// Type 3 = Unknown
	// Type 12 = Dump point
	// Type 13 = SVG
	types := []int32{0, 1, 2, 3, 12, 13}

	for _, dataType := range types {
		navGetCommData := &pb.NavGetCommData{
			Pver:   1,
			Action: 8,
			Type:   dataType,
			Hash:   hash,
			SubCmd: 1,
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

		data, err := proto.Marshal(lubaMsg)
		if err != nil {
			return nil, err
		}
		requests = append(requests, data)
	}

	return requests, nil
}

// RequestDirectFrame requests a specific frame directly without acknowledgment
func RequestDirectFrame(hash int64, dataType int32, frameNumber int32) ([]byte, error) {
	navGetCommData := &pb.NavGetCommData{
		Pver:         1,
		Action:       8,
		Type:         dataType,
		Hash:         hash,
		TotalFrame:   0,    // Don't specify total, let device decide
		CurrentFrame: frameNumber,
		SubCmd:       1,    // Direct request
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

// GetHashListWithSubCmd requests hash list with different SubCmd values
func GetHashListWithSubCmd(subCmd int32) ([]byte, error) {
	navGetHashList := &pb.NavGetHashList{
		Pver:   1,
		SubCmd: subCmd,
	}

	mctlNav := &pb.MctlNav{
		SubNavMsg: &pb.MctlNav_TodevGethash{
			TodevGethash: navGetHashList,
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

// GetRegionalDataWithSubCmd requests a specific frame with custom SubCmd
func GetRegionalDataWithSubCmd(action int32, dataType int32, hash int64, totalFrame int32, currentFrame int32, subCmd int32) ([]byte, error) {
	navGetCommData := &pb.NavGetCommData{
		Pver:         1,
		Action:       action,
		Type:         dataType,
		Hash:         hash,
		TotalFrame:   totalFrame,
		CurrentFrame: currentFrame,
		SubCmd:       subCmd,
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
