package sync

type LayersMocks struct {
	layerCount       uint32
	latestKnownLayer uint32
	layers           []Layer
}

func NewMockLayers() Mesh {
	return &LayersMocks{0, 0, make([]Layer, 100)}
}

func (lm *LayersMocks) GetLayer(i int) (Layer, error) {
	return lm.layers[i-1], nil
}

func (lm *LayersMocks) Close() {

}

func (lm *LayersMocks) GetBlock(layer int, id uint32) (Block, error) {
	l := lm.layers[layer-1]
	return l.Blocks()[0], nil
}

func (lm *LayersMocks) LocalLayerCount() uint32 {
	return lm.layerCount
}

func (lm *LayersMocks) LatestKnownLayer() uint32 {
	return lm.latestKnownLayer
}

func (lm *LayersMocks) SetLatestKnownLayer(idx uint32) {
	lm.latestKnownLayer = idx
}

func (lm *LayersMocks) AddLayer(layer Layer) error {
	lm.layers = append(lm.layers, layer)
	lm.layerCount++
	return nil
}
