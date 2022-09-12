package feishu

func SendText(text string) error {
	return Do(&ReqBase{
		MsgType: "text",
		Content: ReqSendText{
			Text: text,
		},
	})
}
