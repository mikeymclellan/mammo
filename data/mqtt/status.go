package mqtt

type Status struct {
    StatusID string
    State    string
}

func NewStatus(statusID, state string) *Status {
    return &Status{
        StatusID: statusID,
        State:    state,
    }
}

func (s *Status) GetStatusDetails() map[string]string {
    return map[string]string{
        "statusID": s.StatusID,
        "state":    s.State,
    }
}
