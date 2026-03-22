package spec

// MediaType describes a concrete request or response media type.
type MediaType struct {
	ContentType string
	Schema      map[string]any
	Examples    []any
	Encoding    map[string]Encoding
}

// Encoding describes per-property serialization hints for request bodies.
type Encoding struct {
	ContentType   string
	Style         string
	Explode       bool
	AllowReserved bool
}

// ResponseInfo describes a single HTTP response from the spec.
type ResponseInfo struct {
	StatusCode  string
	Description string
	Content     []MediaType
	Headers     []ResponseHeader
}

// ResponseHeader describes a header returned in a response.
type ResponseHeader struct {
	Name        string
	Description string
	Required    bool
	Schema      map[string]any
}

// SecurityInfo describes a security scheme with its full details.
type SecurityInfo struct {
	Name             string
	Type             string
	In               string
	ParameterName    string
	Scheme           string
	BearerFormat     string
	Description      string
	OpenIDConnectURL string
	Scopes           []string
}

// SecurityRequirement describes a logical AND of security schemes.
// Multiple entries on Endpoint.SecurityRequirements are OR alternatives.
type SecurityRequirement struct {
	Schemes []SecurityInfo
}

// ServerInfo describes a resolved server entry from the OpenAPI document.
type ServerInfo struct {
	URL         string
	Description string
}

// Endpoint represents a parsed API endpoint from the spec.
type Endpoint struct {
	Method               string
	Path                 string
	OperationID          string
	Summary              string
	Description          string
	Tags                 []string
	PathParams           []Param
	QueryParams          []Param
	HeaderParams         []Param
	CookieParams         []Param
	RequestBody          *RequestBody
	Security             []string
	Deprecated           bool
	Responses            []ResponseInfo
	SecurityInfo         []SecurityInfo
	SecurityRequirements []SecurityRequirement
	ExternalDocs         string
	BaseURL              string
	Servers              []ServerInfo
}

// Param describes an API parameter.
type Param struct {
	Name            string
	Description     string
	Required        bool
	Type            string
	Default         any
	Enum            []any
	Format          string
	Minimum         *float64
	Maximum         *float64
	MinLength       *uint64
	MaxLength       *uint64
	Style           string
	Explode         bool
	AllowReserved   bool
	AllowEmptyValue bool
	Deprecated      bool
	Schema          map[string]any
	ContentType     string
	Examples        []any
}

// RequestBody describes the request body schema.
type RequestBody struct {
	Required bool
	Content  []MediaType
}
