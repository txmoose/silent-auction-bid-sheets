package main

import (
	"github.com/mcnijman/go-emailaddress"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"html/template"
	"log"
	"os"
	"strconv"
)

// TODO:
// - Actual Functions
//   - Present Bid Sheet
//   - Accept/Reject Bid
//   - Admin functions
//     - Show High Bid For Item
//     - Set Auction End Time
//     - Export all bids to Excel/CSV
//     - Batch Import Items
//     - Generate QR Codes For Items
// - Basic Auth for admin and metrics endpoints

// NOTES:
// Got DB query working, but it still delivers an "empty" page on not-found IDs, need to fix
// Bid inserts work, need logic for high bid, rejecting bids after cutoff, etc...

import (
	"fmt"
	"github.com/gorilla/mux"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"net/http"
)

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
	Name      string
	Email     string
	ItemID    uint
}

type IndexTemplateData struct {
	Event string
}

type ItemTemplateData struct {
	Item Item
	Bid  Bid
}

type ItemBidFormData struct {
	AuctionID  string
	BidAmount  string
	BidderName string
	Email      string
}

var (
	Event     string
	db        *gorm.DB
	err       error
	indexReqs = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "auction_index_total",
			Help: "Counter for hits on index page.",
		})
	itemReqs = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "auction_item_total",
			Help: "Counter for hits on item page.",
		},
		[]string{"item"},
	)
	adminReqs = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "auction_admin_total",
			Help: "Counter for hits on admin page.",
		})
	HighBid = Bid{
		AuctionID: 333,
		BidAmount: 80,
	}
)

func indexHandler(w http.ResponseWriter, r *http.Request) {
	tmpl := template.Must(template.ParseFiles("templates/index.html"))
	tmplData := IndexTemplateData{Event: Event}
	tmpl.Execute(w, tmplData)
	indexReqs.Inc()
	log.Printf("[INFO] Hello World from %s", r.RemoteAddr)
}

func itemHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		vars := mux.Vars(r)
		tmpl := template.Must(template.ParseFiles("templates/item.html"))
		itemID := vars["itemID"]
		itemIDInt, _ := strconv.Atoi(itemID)
		item := Item{}
		db.First(&item, itemIDInt)
		itemTemplateData := ItemTemplateData{
			Item: item,
			Bid:  HighBid,
		}
		tmpl.Execute(w, itemTemplateData)
		itemReqs.WithLabelValues(itemID).Inc()
		log.Printf("[INFO] Item %d requested by %s", item.ID, r.RemoteAddr)

	} else if r.Method == http.MethodPost {
		vars := mux.Vars(r)
		ItemID, err := strconv.ParseUint(vars["itemID"], 10, 32)
		if err != nil {
			log.Fatal("Item ID was broken")
		}

		AuctionID, err := strconv.ParseUint(r.FormValue("AuctionID"), 10, 32)
		if err != nil {
			log.Fatal("Auction ID was not a number")
		}

		BidAmount, err := strconv.ParseUint(r.FormValue("BidAmount"), 10, 32)
		if err != nil {
			log.Fatal("Bid Amount was not a number")
		}

		email, err := emailaddress.Parse(r.FormValue("Email"))
		if err != nil {
			log.Fatal("That is not a valid email address")
		}
		newBid := Bid{
			AuctionID: uint(AuctionID),
			BidAmount: uint(BidAmount),
			Name:      r.FormValue("BidderName"),
			Email:     email.String(),
			ItemID:    uint(ItemID),
		}
		db.Create(&newBid)
	} else {
		fmt.Fprint(w, "Stop that")
	}
}

func adminHandler(w http.ResponseWriter, r *http.Request) {
	adminReqs.Inc()
	log.Printf("[INFO] Admin Request from %s", r.RemoteAddr)
	fmt.Fprint(w, "Hello World! Admin Page")
}

func init() {
	prometheus.Register(itemReqs)
	Event = os.Getenv("AUCTION_EVENT")
	if Event == "" {
		log.Fatal("[FATAL] Event environment variable not set.")
	}
	log.Printf("[INFO] Event Title is %s", Event)
}

func main() {
	log.Print("Setting up Database Connection")
	dsn := "auction:auction_pass@tcp(127.0.0.1:3306)/auction?charset=utf8mb4&parseTime=True&loc=Local"
	db, err = gorm.Open(mysql.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatal("[FATAL] Failed to connect to database")
	}
	log.Print("Database Connection Successful")

	log.Print("Migrating Tables Beginning")
	db.AutoMigrate(&Item{})
	db.AutoMigrate(&Bid{})
	log.Print("Migrating Tables Complete")

	firstItem := Item{
		Name:        "PS5",
		Value:       500,
		ProvidedBy:  "Sony",
		Description: "A PlayStation 5",
	}

	db.Create(&firstItem)

	log.Printf("Item Created %v", firstItem.ID)

	router := mux.NewRouter()
	router.HandleFunc("/", indexHandler).Methods("GET")
	router.HandleFunc("/admin", adminHandler).Methods("GET", "POST")
	router.Handle("/metrics", promhttp.Handler())
	router.HandleFunc(fmt.Sprintf("/%s/{itemID}", Event), itemHandler).Methods("GET", "POST")

	log.Fatal(http.ListenAndServe(":8000", router))
}
