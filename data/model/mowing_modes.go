package model

type MowingMode int

const (
    ModeUnknown MowingMode = iota
    ModeAutomatic
    ModeManual
)

func (mm MowingMode) String() string {
    return [...]string{"Unknown", "Automatic", "Manual"}[mm]
}
