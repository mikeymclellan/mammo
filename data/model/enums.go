package model

type DeviceStatus int

const (
    StatusUnknown DeviceStatus = iota
    StatusActive
    StatusInactive
)

func (ds DeviceStatus) String() string {
    return [...]string{"Unknown", "Active", "Inactive"}[ds]
}
