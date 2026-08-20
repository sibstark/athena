package bitrix

type Cursor struct {
	Position int `json:"position"`
	ID       int `json:"id"`
}

type Workspace struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Position    int    `json:"position"`
	PolicyLevel string `json:"policyLevel"`
	CreatedBy   int    `json:"createdBy"`
	UpdatedBy   int    `json:"updatedBy"`
	CreatedAt   string `json:"createdAt"`
	UpdatedAt   string `json:"updatedAt"`
}

type WorkspaceListResult struct {
	Items      []Workspace `json:"items"`
	NextCursor *Cursor     `json:"nextCursor"`
}

type TreeNode struct {
	ID           int        `json:"id"`
	CollectionID int        `json:"collectionId"`
	ParentID     *int       `json:"parentId"`
	Title        string     `json:"title"`
	Position     int        `json:"position"`
	Children     []TreeNode `json:"children"`
}

type DocumentTreeResult struct {
	Items     []TreeNode `json:"items"`
	Truncated bool       `json:"truncated"`
}

type Document struct {
	ID           int    `json:"id"`
	CollectionID *int   `json:"collectionId"`
	ParentID     *int   `json:"parentId"`
	Title        string `json:"title"`
	Markdown     string `json:"markdown"`
	Position     int    `json:"position"`
	CreatedBy    int    `json:"createdBy"`
	UpdatedBy    int    `json:"updatedBy"`
	CreatedAt    string `json:"createdAt"`
	UpdatedAt    string `json:"updatedAt"`
}

type DocumentGetResult struct {
	Item Document `json:"item"`
}
