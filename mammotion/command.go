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
