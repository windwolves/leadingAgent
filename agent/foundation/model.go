package foundation

type Model struct {
	/** The name of the model to use. */
	Name string `json:"name"`
	/** The provider of the model to use. */
	Provider string `json:"provider"`
	/** The options for the model. */
	Options map[string]interface{} `json:"options"`
}
