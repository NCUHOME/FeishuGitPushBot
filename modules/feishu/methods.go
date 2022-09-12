package feishu

func SendText(text string) error {
	return Do(&ReqBase{
		MsgType: "text",
		Content: ReqSendText{
			Text: text,
		},
	})
}

func SendPostText(title string, contents []ReqSendPostTextContent) error {
	return Do(&ReqBase{
		MsgType: "post",
		Content: map[string]interface{}{
			"post": map[string]interface{}{
				"zh_cn": map[string]interface{}{
					"title": title,
					"content": []interface{}{
						contents,
					},
				},
			},
		},
	})
}

func SendCardMsg(conf *ReqCardMsg) error {
	return Do(&ReqBase{
		MsgType: "interactive",
		Card:    conf,
	})
}
