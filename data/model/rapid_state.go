package model

type RapidState struct {
    StateID string
    Status  string
}

func NewRapidState(stateID, status string) *RapidState {
    return &RapidState{
        StateID: stateID,
        Status:  status,
    }
}

func (rs *RapidState) GetStateDetails() map[string]string {
    return map[string]string{
        "stateID": rs.StateID,
        "status":  rs.Status,
    }
}
