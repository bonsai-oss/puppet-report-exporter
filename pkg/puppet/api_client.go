package puppet

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
)

type ApiClient struct {
	url *url.URL
}

type ApiClientOption func(client *ApiClient) error

// WithUrl - ApiClientOption set the URL of the PuppetDB API
func WithUrl(uri string) ApiClientOption {
	return func(client *ApiClient) error {
		parsedURL, parseError := url.Parse(uri)
		if parseError != nil {
			return parseError
		}
		client.url = parsedURL
		return nil
	}
}

func NewApiClient(options ...ApiClientOption) *ApiClient {
	client := &ApiClient{}
	for _, opt := range options {
		if optionError := opt(client); optionError != nil {
			log.Println(optionError)
		}
	}

	return client
}

// GetNodes - Get all nodes from the PuppetDB API
func (client *ApiClient) GetNodes() ([]Node, error) {
	var nodes []Node
	response, err := http.Get(client.url.JoinPath("pdb/query/v4/nodes").String())
	if err != nil {
		return nil, err
	}
	if response.StatusCode != 200 {
		return nil, fmt.Errorf("unexpected status code %d", response.StatusCode)
	}

	decodeError := json.NewDecoder(response.Body).Decode(&nodes)
	if decodeError != nil {
		return nil, decodeError
	}

	return nodes, err
}

func (client *ApiClient) GetReportHashInfo(hash string) ([]ReportLogEntry, error) {
	response, reportFetchError := http.Get(client.url.JoinPath("pdb/query/v4/reports", hash, "logs").String())
	if reportFetchError != nil {
		return nil, reportFetchError
	}

	var report []ReportLogEntry
	decodeError := json.NewDecoder(response.Body).Decode(&report)
	if decodeError != nil {
		return nil, decodeError
	}
	defer response.Body.Close()

	return report, nil
}
