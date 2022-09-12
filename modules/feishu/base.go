package feishu

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"github.com/Mmx233/tool"
	"github.com/ncuhome/FeishuGitPushBot/global"
	"github.com/ncuhome/FeishuGitPushBot/util"
	"time"
)

func Do(body *ReqBase) error {
	//签名
	t := time.Now().Unix()
	body.Timestamp = fmt.Sprint(t)
	var e error
	body.Sign, e = GenSign(global.Config.Feishu.Secret, t)
	if e != nil {
		return e
	}

	//发送请求
	res, e := util.Http.PostRequest(&tool.DoHttpReq{
		Url:  global.Config.Feishu.Webhook,
		Body: body,
	})
	if e != nil {
		return e
	}
	defer res.Body.Close()

	if res.StatusCode > 299 {
		return fmt.Errorf("feishu webhook return http code %d", res.StatusCode)
	}

	return nil
}

func GenSign(secret string, timestamp int64) (string, error) {
	//timestamp + key 做sha256, 再进行base64 encode
	stringToSign := fmt.Sprintf("%v", timestamp) + "\n" + secret
	var data []byte
	h := hmac.New(sha256.New, []byte(stringToSign))
	_, err := h.Write(data)
	if err != nil {
		return "", err
	}
	signature := base64.StdEncoding.EncodeToString(h.Sum(nil))
	return signature, nil
}
