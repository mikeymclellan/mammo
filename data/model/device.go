package model

type Device struct {
    ID   string
    Name string
}

func NewDevice(id, name string) *Device {
    return &Device{
        ID:   id,
        Name: name,
    }
}

func (d *Device) GetDeviceInfo() map[string]string {
    return map[string]string{
        "id":   d.ID,
        "name": d.Name,
    }
}

