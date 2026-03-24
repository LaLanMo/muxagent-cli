package taskconfig

func (t Topology) Node(name string) NodeRef {
	for _, node := range t.Nodes {
		if node.Name == name {
			return node
		}
	}
	return NodeRef{}
}

func (n NodeRef) JoinOrDefault() Join {
	if n.Join == "" {
		return JoinAll
	}
	return n.Join
}
