package discovery

//var emptyNodeInfo = NodeInfo{} // nil values for everything
//
//
//
//
//func NodeInfoFromNode(nd node.Node, udpAddress string) NodeInfo {
//	raw := nd.Address()
//	ip, _, err := net.SplitHostPort(raw)
//	if err != nil {
//		return emptyNodeInfo
//	}
//	return NodeInfo{nd.PublicKey(), nd, udpAddress, net.ParseIP(ip)}
//}
