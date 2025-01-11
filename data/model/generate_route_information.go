package model

type RouteInformation struct {
    RouteID string
    Details map[string]string
}

func NewRouteInformation(routeID string, details map[string]string) *RouteInformation {
    return &RouteInformation{
        RouteID: routeID,
        Details: details,
    }
}

func (ri *RouteInformation) GetRouteDetails() map[string]string {
    return ri.Details
}
