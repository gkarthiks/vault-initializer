package utility

import (
	"bytes"
	"encoding/json"
	log "github.com/sirupsen/logrus"
	"io/ioutil"
	"net/http"
)

func parseJSONRespo(respJSON []byte, structType interface{}) {
	if respJSON != nil {
		json.Unmarshal(respJSON, &structType)
	}
	return
}

// FireRequest fires the request based on the parameters to the provided URL
func FireRequest(payloadJSON string, url string, reqHeaders map[string]string, method string) ([]byte, error) {
	var req *http.Request

	if len(payloadJSON) > 0 {
		req, err = http.NewRequest(method, url, bytes.NewBuffer([]byte(payloadJSON)))
		log.Debugf("JSON String getting passed as payload: %s to the URL %s", payloadJSON, url)
		req.Header.Set("Content-Type", "application/json")
	} else {
		log.Debug("No payload to pass")
		req, err = http.NewRequest(method, url, nil)
	}
	for key, val := range reqHeaders {
		req.Header.Set(key, val)
	}
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := ioutil.ReadAll(resp.Body)
	//log.Debugf("Response body getting returned: %s", string(body))
	return body, nil
}
