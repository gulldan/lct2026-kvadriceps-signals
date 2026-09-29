package domain

type Chunk struct {
	Index   int
	Text    string
	Section string
}

type Splitter interface {
	Split(text string) []Chunk
}
