package agent

import "net/rpc"

type AgentDelegate struct {
	Address string
	Client  *rpc.Client
}

func NewAgentDelegate(address string) (*AgentDelegate, error) {
	client, err := rpc.DialHTTP("tcp", address)
	if err != nil {
		return nil, err
	}
	agentDelegate := &AgentDelegate{
		Address: address,
		Client:  client,
	}
	return agentDelegate, nil
}
