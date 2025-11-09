package cmd

import (
	"fmt"
	"os"
	"sync"
	"time"

	"mammo/aliyuniot"
	"mammo/auth"
	"mammo/mammotion"

	"github.com/spf13/cobra"
)

var username string
var password string

func Login() {

	client, err := auth.ConnectHTTP(username, password)
	if err != nil {
		fmt.Println("Error logging in:", err)
		return
	}

	if client.LoginInfo == nil {
		fmt.Println("Error logging in: LoginInfo is nil")
		return
	}

	fmt.Println("Logged in successfully:", client.LoginInfo.UserInformation.Email)

	country_code := client.LoginInfo.UserInformation.DomainAbbreviation

	cg := aliyuniot.NewCloudIOTGateway()
	_, err = cg.GetRegion(country_code, client.LoginInfo.AuthorizationCode)

	if err != nil {
		fmt.Println("Error getting region:", err)
		return
	}
	fmt.Printf("Region from API: %s\n", cg.RegionResponse.Data.RegionId)

	// Keep ap-southeast-1 for auth/tokens, but MQTT will use cn-shanghai
	err = cg.Connect()
	if err != nil {
		fmt.Println("Error connecting to cloud:", err)
		return
	}
	_, err = cg.LoginByOAuth(country_code, client.LoginInfo.AuthorizationCode)

	if err != nil {
		fmt.Println("IOT login error:", err)
		return
	}
	fmt.Println("IOT login successfull")

	err = cg.AepHandle()
	if err != nil {
		fmt.Println("Error handling AEP:", err)
		return
	}
	fmt.Println("AEP handled successfully")

	err = cg.SessionByAuthCode()
	if err != nil {
		fmt.Println("Error getting session by auth code:", err)
		return
	}
	fmt.Println("Session by auth code successful")

	// 1. Get the device list (this uses the session token from SessionByAuthCode)
	devices, err := cg.ListDevices()
	if err != nil {
		fmt.Println("Error getting devices:", err)
		return
	}

	if len(devices) == 0 {
		fmt.Println("No devices found")
		return
	}
	fmt.Println("Devices found:")
	for _, device := range devices {
		fmt.Println("    Nickname:", device.NickName)
		fmt.Println("    Device ID:", device.DeviceName)
		fmt.Println("    Model:", device.ProductName)
		fmt.Println("    Status:", device.Status)
		fmt.Println("")
	}

	// Refresh the session token to ensure it's valid for MQTT binding
	fmt.Printf("Before refresh - iotToken (first 20 chars): %s...\n", cg.SessionByAuthCodeResponse.Data.IotToken[:20])
	err = cg.CheckOrRefreshSession()
	if err != nil {
		fmt.Println("Error refreshing session:", err)
		return
	}
	fmt.Printf("After refresh - iotToken (first 20 chars): %s... (expires: %d)\n", cg.SessionByAuthCodeResponse.Data.IotToken[:20], cg.SessionByAuthCodeResponse.Data.IotTokenExpire)

	// 2. Create MQTT and Cloud clients with token from SessionByAuthCode
	fmt.Printf("AEP ProductKey: %s\n", cg.AepResponse.Data.ProductKey)
	fmt.Printf("AEP DeviceName: %s\n", cg.AepResponse.Data.DeviceName)
	fmt.Printf("AEP DeviceSecret: %s\n", cg.AepResponse.Data.DeviceSecret)

	mqttClient := mammotion.NewMammotionMQTT(
		cg.RegionResponse.Data.RegionId,
		cg.AepResponse.Data.ProductKey,
		cg.AepResponse.Data.DeviceName,
		cg.AepResponse.Data.DeviceSecret,
		cg.SessionByAuthCodeResponse.Data.IotToken, // Use fresh token immediately
		cg,
	)
	mammoCloud := mammotion.NewMammotionCloud(mqttClient, cg)

	// 4. Connect MQTT client
	var wg sync.WaitGroup
	wg.Add(1)
	mqttClient.OnReady = func() {
		wg.Done()
	}
	mammoCloud.ConnectAsync()
	wg.Wait()

	// 4.5 Get first device (don't subscribe to Luba topics, responses come via AEP)
	firstDevice := devices[0]
	fmt.Printf("Using device: %s (IotID: %s)\n", firstDevice.DeviceName, firstDevice.IotId)

	// 4.6 Create the device-specific objects BEFORE sending commands
	mowingDevice := mammotion.NewMowingDevice(&firstDevice, *cg, mammoCloud)
	stateManager := mammotion.NewStateManager(mowingDevice)
	mammotion.NewMammotionBaseCloudDevice(mammoCloud, mowingDevice, stateManager)

	propertiesReceived := make(chan struct{})
	stateManager.OnPropertiesReceived = func() {
		close(propertiesReceived)
	}

	// 4.7 Send ble_sync command to trigger device reporting
	fmt.Printf("Sending ble_sync command to activate device reporting...\n")
	bleSyncData, err := mammotion.SendTodevBleSync(3)
	if err != nil {
		fmt.Printf("Error creating ble_sync command: %v\n", err)
		return
	}
	msgId, err := cg.SendCloudCommand(firstDevice.IotId, bleSyncData)
	if err != nil {
		fmt.Printf("Error sending ble_sync command: %v\n", err)
		return
	}
	fmt.Printf("ble_sync command sent successfully (message ID: %s)\n", msgId)

	// 4.8 Send get_report_cfg command via HTTP API (not MQTT)
	fmt.Printf("Sending get_report_cfg command to device: %s (IotID: %s)\n", firstDevice.DeviceName, firstDevice.IotId)
	reportCfgData, err := mammotion.GetReportCfg(10000, 1000, 2000)
	if err != nil {
		fmt.Printf("Error creating get_report_cfg command: %v\n", err)
		return
	}

	msgId, err = cg.SendCloudCommand(firstDevice.IotId, reportCfgData)
	if err != nil {
		fmt.Printf("Error sending get_report_cfg command: %v\n", err)
		return
	}
	fmt.Printf("get_report_cfg command sent successfully (message ID: %s)\n", msgId)

	fmt.Println("Waiting for device properties (will wait up to 2 minutes)...")

	select {
	case <-propertiesReceived:
		fmt.Printf("✅ Battery Level: %d%%\n", mowingDevice.BatteryPercentage)
	case <-time.After(2 * time.Minute):
		fmt.Println("⏱️  Timed out waiting for device properties after 2 minutes.")
		fmt.Println("The command was sent successfully, but no MQTT response was received.")
		fmt.Println("This could mean:")
		fmt.Println("  - The device needs more time to respond")
		fmt.Println("  - The device needs to be actively running/awake")
		fmt.Println("  - Additional setup commands are needed first")
	}
}

var rootCmd = &cobra.Command{
	Use:   "mammo",
	Short: "A CLI test app for Mammotion devices",
	Long: `This CLI tool is designed for interacting with the Mammotion APIs and MQTT commands.

Examples and usage of this application include:

- Sending MQTT commands to control devices
- Fetching data from Mammotion APIs
- Subscribing to MQTT topics to receive real-time updates
- Managing device configurations and settings`,
}

var batteryCmd = &cobra.Command{
	Use:   "battery",
	Short: "Get the battery level of the device",
	Run: func(cmd *cobra.Command, args []string) {
		client, err := auth.ConnectHTTP(username, password)
		if err != nil {
			fmt.Println("Error logging in:", err)
			return
		}
		if client.LoginInfo == nil {
			fmt.Println("Error logging in: LoginInfo is nil")
			return
		}

		cg := aliyuniot.NewCloudIOTGateway()
		_, err = cg.GetRegion(client.LoginInfo.UserInformation.DomainAbbreviation, client.LoginInfo.AuthorizationCode)
		if err != nil {
			fmt.Println("Error getting region:", err)
			return
		}

		err = cg.Connect()
		if err != nil {
			fmt.Println("Error connecting to cloud:", err)
			return
		}
		_, err = cg.LoginByOAuth(client.LoginInfo.UserInformation.DomainAbbreviation, client.LoginInfo.AuthorizationCode)
		if err != nil {
			fmt.Println("IOT login error:", err)
			return
		}

		err = cg.AepHandle()
		if err != nil {
			fmt.Println("Error handling AEP:", err)
			return
		}

		err = cg.SessionByAuthCode()
		if err != nil {
			fmt.Println("Error getting session by auth code:", err)
			return
		}

		devices, err := cg.ListDevices()
		if err != nil {
			fmt.Println("Error getting devices:", err)
			return
		}

		err = cg.CheckOrRefreshSession()
		if err != nil {
			fmt.Println("Error refreshing session:", err)
			return
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
		mqttClient.OnReady = func() {
			wg.Done()
		}
		mammoCloud.ConnectAsync()
		wg.Wait()

		if len(devices) == 0 {
			fmt.Println("No devices found")
			return
		}
		firstDevice := devices[0]
		mowingDevice := mammotion.NewMowingDevice(&firstDevice, *cg, mammoCloud)
		stateManager := mammotion.NewStateManager(mowingDevice)
		mammotion.NewMammotionBaseCloudDevice(mammoCloud, mowingDevice, stateManager)

		propertiesReceived := make(chan struct{})
		stateManager.OnPropertiesReceived = func() {
			close(propertiesReceived)
		}

		fmt.Println("Waiting for device properties...")

		select {
		case <-propertiesReceived:
			fmt.Printf("Battery Level: %d%%\n", mowingDevice.BatteryPercentage)
		case <-time.After(30 * time.Second):
			fmt.Println("Timed out waiting for device properties.")
		}
	},
}

var loginCmd = &cobra.Command{
	Use:   "login",
	Short: "Login to the Mammotion API",
	Run: func(cmd *cobra.Command, args []string) {
		Login()
	},
}

var dockCmd = &cobra.Command{
	Use:   "dock",
	Short: "Dock the device",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("wooooof!")
	},
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	rootCmd.AddCommand(batteryCmd)
	rootCmd.AddCommand(loginCmd)
	rootCmd.AddCommand(dockCmd)
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

func init() {
	// Here you will define your flags and configuration settings.
	// Cobra supports persistent flags, which, if defined here,
	// will be global for your application.

	//rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.luba.yaml)")

	// Cobra also supports local flags, which will only run
	// when this action is called directly.
	rootCmd.PersistentFlags().StringVarP(&username, "username", "u", "", "Username for login")
	rootCmd.PersistentFlags().StringVarP(&password, "password", "p", "", "Password for login")
}

