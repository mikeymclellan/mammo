package cmd

import (
	"fmt"
	"os"
	"sync"
	"time"

	"mammo/aliyuniot"
	"mammo/auth"
	"mammo/mammotion"
	pb "mammo/proto"

	"github.com/spf13/cobra"
	"google.golang.org/protobuf/proto"
)

// cloudSession holds the connected device and gateway for control commands.
type cloudSession struct {
	gateway      *aliyuniot.CloudIOTGateway
	mqttClient   *mammotion.MammotionMQTT
	device       *aliyuniot.Device
	mowingDevice *mammotion.MowingDevice
	stateManager *mammotion.StateManager
}

func (s *cloudSession) Close() {
	if s.mqttClient != nil {
		s.mqttClient.Disconnect()
	}
}

func connectCloud() (*cloudSession, error) {
	client, err := auth.ConnectHTTP(username, password)
	if err != nil {
		return nil, fmt.Errorf("login: %w", err)
	}
	if client.LoginInfo == nil {
		return nil, fmt.Errorf("login: LoginInfo nil")
	}

	cg := aliyuniot.NewCloudIOTGateway()
	if _, err := cg.GetRegion(client.LoginInfo.UserInformation.DomainAbbreviation, client.LoginInfo.AuthorizationCode); err != nil {
		return nil, fmt.Errorf("region: %w", err)
	}
	if err := cg.Connect(); err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	if _, err := cg.LoginByOAuth(client.LoginInfo.UserInformation.DomainAbbreviation, client.LoginInfo.AuthorizationCode); err != nil {
		return nil, fmt.Errorf("oauth: %w", err)
	}
	if err := cg.AepHandle(); err != nil {
		return nil, fmt.Errorf("aep: %w", err)
	}
	if err := cg.SessionByAuthCode(); err != nil {
		return nil, fmt.Errorf("session: %w", err)
	}
	devices, err := cg.ListDevices()
	if err != nil {
		return nil, fmt.Errorf("list devices: %w", err)
	}
	if len(devices) == 0 {
		return nil, fmt.Errorf("no devices")
	}
	if err := cg.CheckOrRefreshSession(); err != nil {
		return nil, fmt.Errorf("refresh: %w", err)
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
	mammotion.NewMammotionBaseCloudDevice(mammoCloud, mowingDevice, stateManager)

	return &cloudSession{
		gateway:      cg,
		mqttClient:   mqttClient,
		device:       &firstDevice,
		mowingDevice: mowingDevice,
		stateManager: stateManager,
	}, nil
}

// primeSession sends BLE sync + report-cfg so the device starts reporting.
func primeSession(s *cloudSession) error {
	if err := s.gateway.CheckOrRefreshSession(); err != nil {
		return fmt.Errorf("refresh: %w", err)
	}
	bleSyncData, err := mammotion.SendTodevBleSync(3)
	if err != nil {
		return err
	}
	if _, err := s.gateway.SendCloudCommand(s.device.IotId, bleSyncData); err != nil {
		return fmt.Errorf("ble_sync: %w", err)
	}
	reportCfgData, err := mammotion.GetReportCfg(10000, 1000, 1000)
	if err != nil {
		return err
	}
	if _, err := s.gateway.SendCloudCommand(s.device.IotId, reportCfgData); err != nil {
		return fmt.Errorf("report_cfg: %w", err)
	}
	return nil
}

// startPolling sends GetReportCfg every second in the background to keep
// position/property updates flowing. The returned stop func ends polling.
func startPolling(s *cloudSession) func() {
	stop := make(chan struct{})
	go func() {
		t := time.NewTicker(1 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				pollData, err := mammotion.GetReportCfg(10000, 1000, 1000)
				if err != nil {
					continue
				}
				_, _ = s.gateway.SendCloudCommand(s.device.IotId, pollData)
			}
		}
	}()
	return func() { close(stop) }
}

// withSession connects, primes the device, runs fn, then disconnects cleanly.
// Errors are printed and exit non-zero after the deferred disconnect runs.
func withSession(fn func(*cloudSession) error) {
	run := func() error {
		s, err := connectCloud()
		if err != nil {
			return fmt.Errorf("connect: %w", err)
		}
		defer s.Close()
		if err := primeSession(s); err != nil {
			return fmt.Errorf("prime: %w", err)
		}
		return fn(s)
	}
	if err := run(); err != nil {
		fmt.Println("Error:", err)
		os.Exit(1)
	}
}

// buildNav wraps a nav sub-message in the standard app→main-controller LubaMsg envelope.
func buildNav(nav *pb.MctlNav) ([]byte, error) {
	lubaMsg := &pb.LubaMsg{
		Msgtype:    pb.MsgCmdType_MSG_CMD_TYPE_NAV,
		Sender:     pb.MsgDevice_DEV_MOBILEAPP,
		Rcver:      pb.MsgDevice_DEV_MAINCTL,
		Msgattr:    pb.MsgAttr_MSG_ATTR_REQ,
		Seqs:       1,
		Version:    1,
		Subtype:    1,
		Timestamp:  uint64(time.Now().UnixMilli()),
		LubaSubMsg: &pb.LubaMsg_Nav{Nav: nav},
	}
	return proto.Marshal(lubaMsg)
}

func sendNav(s *cloudSession, nav *pb.MctlNav) error {
	data, err := buildNav(nav)
	if err != nil {
		return err
	}
	_, err = s.gateway.SendCloudCommand(s.device.IotId, data)
	return err
}

var rechargeCmd = &cobra.Command{
	Use:   "recharge",
	Short: "Send the mower back to the charging dock (todev_rechgcmd)",
	Run: func(cmd *cobra.Command, args []string) {
		withSession(func(s *cloudSession) error {
			err := sendNav(s, &pb.MctlNav{
				SubNavMsg: &pb.MctlNav_TodevRechgcmd{TodevRechgcmd: 1},
			})
			if err != nil {
				return err
			}
			fmt.Println("Recharge command sent. Mower should head for the dock.")
			time.Sleep(2 * time.Second)
			return nil
		})
	},
}

var cancelCmd = &cobra.Command{
	Use:   "cancel",
	Short: "Cancel the current sub-task (todev_cancel_suscmd)",
	Run: func(cmd *cobra.Command, args []string) {
		withSession(func(s *cloudSession) error {
			err := sendNav(s, &pb.MctlNav{
				SubNavMsg: &pb.MctlNav_TodevCancelSuscmd{TodevCancelSuscmd: 1},
			})
			if err != nil {
				return err
			}
			fmt.Println("Cancel sub-task command sent.")
			time.Sleep(2 * time.Second)
			return nil
		})
	},
}

var leavePileCmd = &cobra.Command{
	Use:   "leave-pile",
	Short: "Send one-touch leave-pile (useful if stuck near dock)",
	Run: func(cmd *cobra.Command, args []string) {
		withSession(func(s *cloudSession) error {
			err := sendNav(s, &pb.MctlNav{
				SubNavMsg: &pb.MctlNav_TodevOneTouchLeavePile{TodevOneTouchLeavePile: 1},
			})
			if err != nil {
				return err
			}
			fmt.Println("Leave-pile command sent.")
			time.Sleep(2 * time.Second)
			return nil
		})
	},
}

var (
	moveLinear   int32
	moveAngular  int32
	moveDuration int
)

var moveCmd = &cobra.Command{
	Use:   "move",
	Short: "Drive the mower for a fixed duration (non-interactive). Use --linear/--angular/--duration.",
	Long: `Send continuous motion commands at 200ms intervals for the requested duration.
Typical values:
  --linear  1000 = forward, -1000 = backward
  --angular 450  = right,   -450  = left
  --duration in seconds.`,
	Run: func(cmd *cobra.Command, args []string) {
		withSession(func(s *cloudSession) error {
			// Position callback for live feedback.
			var posMu sync.Mutex
			var lastX, lastY float32
			var lastAngle int32
			var lastPosType int32
			var posUpdates int
			s.stateManager.OnPositionUpdate = func(x, y float32, angle int32, posType int32) {
				posMu.Lock()
				defer posMu.Unlock()
				lastX, lastY, lastAngle, lastPosType = x, y, angle, posType
				posUpdates++
			}

			stopPolling := startPolling(s)
			defer stopPolling()

			fmt.Printf("Driving linear=%d angular=%d for %ds...\n", moveLinear, moveAngular, moveDuration)
			endTime := time.Now().Add(time.Duration(moveDuration) * time.Second)
			ticker := time.NewTicker(200 * time.Millisecond)
			defer ticker.Stop()
			var lastPrint time.Time

			for time.Now().Before(endTime) {
				if err := s.gateway.CheckOrRefreshSession(); err != nil {
					fmt.Println("Session refresh error:", err)
					break
				}
				data, err := mammotion.SendMotionControl(moveLinear, moveAngular)
				if err != nil {
					fmt.Println("Build motion error:", err)
					break
				}
				if _, err := s.gateway.SendCloudCommand(s.device.IotId, data); err != nil {
					fmt.Println("Send motion error:", err)
					break
				}
				<-ticker.C
				if time.Since(lastPrint) >= time.Second {
					posMu.Lock()
					fmt.Printf("  pos X=%.0f Y=%.0f angle=%d posType=%d updates=%d battery=%d%%\n",
						lastX, lastY, lastAngle, lastPosType, posUpdates, s.mowingDevice.BatteryPercentage)
					posMu.Unlock()
					lastPrint = time.Now()
				}
			}

			// Always send stop at the end.
			stopData, err := mammotion.StopMotion()
			if err == nil {
				_, _ = s.gateway.SendCloudCommand(s.device.IotId, stopData)
			}
			fmt.Println("Stop command sent.")
			time.Sleep(1 * time.Second)
			return nil
		})
	},
}

var positionDuration int

var positionCmd = &cobra.Command{
	Use:   "position",
	Short: "Print position updates for a fixed duration",
	Run: func(cmd *cobra.Command, args []string) {
		withSession(func(s *cloudSession) error {
			var posMu sync.Mutex
			var updates int
			s.stateManager.OnPositionUpdate = func(x, y float32, angle int32, posType int32) {
				posMu.Lock()
				updates++
				n := updates
				posMu.Unlock()
				fmt.Printf("[%03d] X=%.0f Y=%.0f angle=%d posType=%d battery=%d%%\n",
					n, x, y, angle, posType, s.mowingDevice.BatteryPercentage)
			}
			stopPolling := startPolling(s)
			defer stopPolling()
			time.Sleep(time.Duration(positionDuration) * time.Second)
			fmt.Printf("Done. %d position updates received.\n", updates)
			return nil
		})
	},
}

var sustaskVal int32

var sustaskCmd = &cobra.Command{
	Use:   "sustask",
	Short: "Send raw todev_sustask with --val (experimental, semantics unconfirmed)",
	Run: func(cmd *cobra.Command, args []string) {
		withSession(func(s *cloudSession) error {
			stopPolling := startPolling(s)
			defer stopPolling()
			err := sendNav(s, &pb.MctlNav{
				SubNavMsg: &pb.MctlNav_TodevSustask{TodevSustask: sustaskVal},
			})
			if err != nil {
				return err
			}
			fmt.Printf("Sent todev_sustask=%d. Watching state for 8s...\n", sustaskVal)
			time.Sleep(8 * time.Second)
			return nil
		})
	},
}

var (
	taskCtrlType   int32
	taskCtrlAction int32
)

var taskCtrlCmd = &cobra.Command{
	Use:   "task-ctrl",
	Short: "Send raw NavTaskCtrl with --type --action (experimental, semantics unconfirmed)",
	Run: func(cmd *cobra.Command, args []string) {
		withSession(func(s *cloudSession) error {
			stopPolling := startPolling(s)
			defer stopPolling()
			err := sendNav(s, &pb.MctlNav{
				SubNavMsg: &pb.MctlNav_TodevTaskctrl{TodevTaskctrl: &pb.NavTaskCtrl{
					Type:   taskCtrlType,
					Action: taskCtrlAction,
				}},
			})
			if err != nil {
				return err
			}
			fmt.Printf("Sent NavTaskCtrl type=%d action=%d. Watching state for 6s...\n", taskCtrlType, taskCtrlAction)
			time.Sleep(6 * time.Second)
			return nil
		})
	},
}

func init() {
	moveCmd.Flags().Int32Var(&moveLinear, "linear", 0, "linear speed (-1000..1000)")
	moveCmd.Flags().Int32Var(&moveAngular, "angular", 0, "angular speed (-450..450)")
	moveCmd.Flags().IntVar(&moveDuration, "duration", 2, "seconds to drive")

	positionCmd.Flags().IntVar(&positionDuration, "duration", 15, "seconds to listen for position updates")

	sustaskCmd.Flags().Int32Var(&sustaskVal, "val", 1, "todev_sustask value")
	taskCtrlCmd.Flags().Int32Var(&taskCtrlType, "type", 0, "NavTaskCtrl.Type")
	taskCtrlCmd.Flags().Int32Var(&taskCtrlAction, "action", 0, "NavTaskCtrl.Action")

	rootCmd.AddCommand(rechargeCmd)
	rootCmd.AddCommand(cancelCmd)
	rootCmd.AddCommand(moveCmd)
	rootCmd.AddCommand(positionCmd)
	rootCmd.AddCommand(leavePileCmd)
	rootCmd.AddCommand(sustaskCmd)
	rootCmd.AddCommand(taskCtrlCmd)
}
