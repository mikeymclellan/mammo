package model

type Location struct {
    Latitude  float64
    Longitude float64
}

func NewLocation(latitude, longitude float64) *Location {
    return &Location{
        Latitude:  latitude,
        Longitude: longitude,
    }
}

func (l *Location) GetCoordinates() map[string]float64 {
    return map[string]float64{
        "latitude":  l.Latitude,
        "longitude": l.Longitude,
    }
}
