package model

type Plan struct {
    PlanID string
    Steps  []string
}

func NewPlan(planID string, steps []string) *Plan {
    return &Plan{
        PlanID: planID,
        Steps:  steps,
    }
}

func (p *Plan) GetPlanDetails() map[string]interface{} {
    return map[string]interface{}{
        "planID": p.PlanID,
        "steps":  p.Steps,
    }
}
