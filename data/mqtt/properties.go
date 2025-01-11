package mqtt

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
