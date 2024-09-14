package elasticsearch

import (
	"fmt"
	"io"
	"net/http"
)

func DrainElasticsearchNode(elasticURL, nodeName, username, password string) error {
	req, err := http.NewRequest("POST", fmt.Sprintf("%s/_cluster/nodes/%s/_shutdown", elasticURL, nodeName), nil)
	if err != nil {
		return err
	}

	req.SetBasicAuth(username, password)
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return fmt.Errorf("error draining node: %s", string(body))
	}

	return nil
}
