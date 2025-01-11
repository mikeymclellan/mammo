package model

type ReportInfo struct {
    ReportID string
    Info     map[string]string
}

func NewReportInfo(reportID string, info map[string]string) *ReportInfo {
    return &ReportInfo{
        ReportID: reportID,
        Info:     info,
    }
}

func (ri *ReportInfo) GetReportDetails() map[string]string {
    return ri.Info
}
