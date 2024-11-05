package v1alpha1

// nodeInfo struct for elasticsearch nodes
type NodeInfo struct {
	IP          string `json:"ip"`
	HeapPercent string `json:"heap.percent"`
	RAMPercent  string `json:"ram.percent"`
	CPU         string `json:"cpu"`
	Load1m      string `json:"load_1m"`
	Load5m      string `json:"load_5m"`
	Load15m     string `json:"load_15m"`
	NodeRole    string `json:"node.role"`
	Master      string `json:"master"`
	Name        string `json:"name"`
}

// shardInfo struct for elasticsearch shards
type ShardInfo struct {
	Index   string `json:"index"`
	Shard   string `json:"shard"`
	PriRep  string `json:"prirep"`
	State   string `json:"state"`
	Docs    string `json:"docs"`
	Store   string `json:"store"`
	Dataset string `json:"dataset"`
	IP      string `json:"ip"`
	Node    string `json:"node"`
}

// settings struct for elasticsearch settings
type ElasticsearchSettings struct {
	Persistent map[string]interface{} `json:"persistent"`
	Transient  map[string]interface{} `json:"transient"`
}
