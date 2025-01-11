package model

type ExecuteBoarder struct {
    BoarderID string
    Params    *ExecuteBoarderParams
}

func NewExecuteBoarder(boarderID string, params *ExecuteBoarderParams) *ExecuteBoarder {
    return &ExecuteBoarder{
        BoarderID: boarderID,
        Params:    params,
    }
}

func (eb *ExecuteBoarder) GetBoarderInfo() map[string]interface{} {
    return map[string]interface{}{
        "boarderID": eb.BoarderID,
        "params":    eb.Params.GetParams(),
    }
}
