package main

type Item struct {
	ID          uint
	Name        string
	ProvidedBy  string
	Description string
	Value       uint
	Bid         []Bid
}

type Bid struct {
	ID        uint
	AuctionID uint
	BidAmount uint
	ItemID    uint
}

type IndexTemplateData struct {
	Event string
}

type ItemTemplateData struct {
	Item   Item
	Bid    Bid
	MinBid uint
}

type AdminTemplateData struct {
	Event string
	Items []ItemTemplateData
}

type ErrorPageData struct {
	Message string
}

type application struct {
	auth struct {
		username string
		password string
	}
}
