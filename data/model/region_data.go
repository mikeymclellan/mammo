package model

type RegionData struct {
    RegionID string
    Data     map[string]string
}

func NewRegionData(regionID string, data map[string]string) *RegionData {
    return &RegionData{
        RegionID: regionID,
        Data:     data,
    }
}

func (rd *RegionData) GetRegionDetails() map[string]string {
    return rd.Data
}
