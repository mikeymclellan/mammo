package mqtt

// Item represents a single property value with a timestamp.
type Item struct {
	Time  int64       `json:"time"`
	Value interface{} `json:"value"`
}

// Items contains the set of all device properties.
// So far, only BatteryPercentage is included.
type Items struct {
	BatteryPercentage Item `json:"batteryPercentage"`
}

// PropertyParams represents the 'params' object in a ThingPropertiesMessage.
type PropertyParams struct {
	IotID string `json:"iotId"`
	Items Items  `json:"items"`
}

// ThingPropertiesMessage is the top-level structure for a device properties message.
type ThingPropertiesMessage struct {
	Params PropertyParams `json:"params"`
}