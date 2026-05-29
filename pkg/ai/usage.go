package ai

// Usage contains token usage statistics for a model response.
type Usage struct {
	Input       int
	Output      int
	CacheRead   int
	CacheWrite  int
	Reasoning   int
	InputAudio  int
	OutputAudio int
	Total       int
	Cost        UsageCost
}

// UsageCost contains cost breakdown in USD.
type UsageCost struct {
	Input       float64
	Output      float64
	CacheRead   float64
	CacheWrite  float64
	Reasoning   float64
	InputAudio  float64
	OutputAudio float64
	Total       float64
}
