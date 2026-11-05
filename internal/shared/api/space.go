package api

// Space is a Space as the API returns it.
type Space struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// SpaceAddRequest records a shared Space under a new name.
type SpaceAddRequest struct {
	Name string `json:"name"`
}

// SpaceAddResponse is the Space space.add recorded.
type SpaceAddResponse struct {
	Space Space `json:"space"`
}

// SpaceListRequest asks for every Space the caller can act in.
type SpaceListRequest struct{}

// SpaceListResponse holds every Space the caller can act in, by name.
type SpaceListResponse struct {
	Spaces []Space `json:"spaces"`
}

// The space operations.
var (
	// SpaceAdd records a shared Space. A name another Space holds is refused
	// with CodeNameTaken.
	SpaceAdd = Op[SpaceAddRequest, SpaceAddResponse]{Name: "space.add", Write: true}
	// SpaceList lists the Spaces the caller can act in.
	SpaceList = Op[SpaceListRequest, SpaceListResponse]{Name: "space.list"}
)

var spaceOperations = []Operation{
	SpaceAdd,
	SpaceList,
}
