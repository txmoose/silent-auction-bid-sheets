package main

import "gorm.io/gorm"

type Item struct {
	gorm.Model
	Name        string
	ProvidedBy  string
	Description string
	Value       uint
	Bid         []Bid
}

type Bid struct {
	gorm.Model
	AuctionID uint
	BidAmount uint
	ItemID    uint
}

type IndexTemplateData struct {
	Event string
}

type ItemTemplateData struct {
	Item Item
	Bid  Bid
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
