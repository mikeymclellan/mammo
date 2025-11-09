package model

type HashList struct {
	Hashes []string
}

type NavGetHashListAck struct {
	// Placeholder
}

func NewHashList(hashes []string) *HashList {
	return &HashList{
		Hashes: hashes,
	}
}

func (hl *HashList) GetHashes() []string {
	return hl.Hashes
}
