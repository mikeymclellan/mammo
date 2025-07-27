package mqtt

type PropertyParams struct {
	IotID string `json:"iotId"`
}

type ThingPropertiesMessage struct {
	Params PropertyParams `json:"params"`
}

type Properties struct {
	PropertyID string
	Values     map[string]string
}

func NewProperties(propertyID string, values map[string]string) *Properties {
	return &Properties{
		PropertyID: propertyID,
		Values:     values,
	}
}

func (p *Properties) GetPropertyValues() map[string]string {
	return p.Values
}
