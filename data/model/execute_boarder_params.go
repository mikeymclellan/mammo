package model

type ExecuteBoarderParams struct {
    Param1 string
    Param2 int
}

func NewExecuteBoarderParams(param1 string, param2 int) *ExecuteBoarderParams {
    return &ExecuteBoarderParams{
        Param1: param1,
        Param2: param2,
    }
}

func (ebp *ExecuteBoarderParams) GetParams() map[string]interface{} {
    return map[string]interface{}{
        "param1": ebp.Param1,
        "param2": ebp.Param2,
    }
}
