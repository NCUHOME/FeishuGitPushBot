package response

type CodeOnly struct {
	Code uint `json:"code"`
}

type WithDynamicData struct {
	Code uint        `json:"code"`
	Data interface{} `json:"data,omitempty"`
}

type WithIdData struct {
	CodeOnly
	Data struct {
		ID uint `json:"id"`
	} `json:"data"`
}

type WithStringData struct {
	CodeOnly
	Data string `json:"data"`
}
