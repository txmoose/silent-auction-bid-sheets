package main

import (
	"github.com/mcnijman/go-emailaddress"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"html/template"
	"log"
	"net"
	"os"
	"strconv"
	"time"
)

// TODO:
// - Actual Functions
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
		[]string{"item", "method"},
	)
	adminReqs = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "auction_admin_total",
			Help: "Counter for hits on admin page.",
		})
	endTimeString = "26 Jan 23 22:15 EST"
)

func indexHandler(w http.ResponseWriter, r *http.Request) {
	tmpl := template.Must(template.ParseFiles("templates/index.html"))
	tmplData := IndexTemplateData{Event: Event}
	_ = tmpl.Execute(w, tmplData)
	indexReqs.Inc()
	log.Printf("[INFO] Hello World from %s", r.RemoteAddr)
}

func itemHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		item := Item{}
		var bid Bid
		vars := mux.Vars(r)
		tmpl := template.Must(template.ParseFiles("templates/item.html"))
		errTmpl := template.Must(template.ParseFiles("templates/error.html"))

		itemID, err := strconv.ParseUint(vars["itemID"], 10, 32)
		if err != nil {
			_ = errTmpl.Execute(w, ErrorPageData{Message: "Invalid Item"})
			log.Fatal("Item ID was broken")
		}

		// Get Item From Database
		db.First(&item, itemID)
		// Get High Bid From Database
		db.Order("bid_amount desc").Find(&bid, "item_id = ?", item.ID).Limit(1)

		// Insert info into HTML Template
		itemTemplateData := ItemTemplateData{
			Item: item,
			Bid:  bid,
		}

		// Execute Template
		_ = tmpl.Execute(w, itemTemplateData)

		// Increment Prometheus Metric Counter
		itemReqs.WithLabelValues(item.Name, r.Method).Inc()

		remoteAddr, _, _ := net.SplitHostPort(r.RemoteAddr)
		log.Printf("[INFO] Item %d requested by %s", item.ID, remoteAddr)

	} else if r.Method == http.MethodPost {
		// Initialize templates
		tmpl := template.Must(template.ParseFiles("templates/thanks.html"))
		errTmpl := template.Must(template.ParseFiles("templates/error.html"))
		endTime, err := time.Parse(time.RFC822, endTimeString)
		if err != nil {
			_ = errTmpl.Execute(w, ErrorPageData{Message: "Something went wrong, please try again"})
			log.Print("Parsing endTime failed")
			return
		}

		if time.Now().After(endTime) {
			_ = errTmpl.Execute(w, ErrorPageData{Message: "I'm sorry, the auction has closed."})
			log.Print("Bid after close time")
			return
		}

		// Get variables from the Mux router
		vars := mux.Vars(r)
		// Read the ItemID
		ItemID, err := strconv.ParseUint(vars["itemID"], 10, 32)
		if err != nil {
			_ = errTmpl.Execute(w, ErrorPageData{Message: "Invalid Item"})
			log.Print("Item ID was broken")
			return
		}

		// Read in items from form
		AuctionID, err := strconv.ParseUint(r.FormValue("AuctionID"), 10, 32)
		if err != nil {
			_ = errTmpl.Execute(w, ErrorPageData{Message: "Auction ID must be a number"})
			log.Print("Auction ID was not a number")
			return
		}
		BidAmount, err := strconv.ParseUint(r.FormValue("BidAmount"), 10, 32)
		if err != nil {
			_ = errTmpl.Execute(w, ErrorPageData{Message: "Bid Amount must be a number"})
			log.Print("Bid Amount was not a number")
			return
		}
		email, err := emailaddress.Parse(r.FormValue("Email"))
		if err != nil {
			_ = errTmpl.Execute(w, ErrorPageData{Message: "Email address appears to be invalid"})
			log.Print("That is not a valid email address")
			return
		}
		// Build a Bid Item from form data
		newBid := Bid{
			AuctionID: uint(AuctionID),
			BidAmount: uint(BidAmount),
			Name:      r.FormValue("BidderName"),
			Email:     email.String(),
			ItemID:    uint(ItemID),
		}
		// Insert new bid into database
		db.Create(&newBid)

		// Get corresponding Item for thanks page
		item := Item{}
		db.First(&item, ItemID)

		// Build message for thanks page
		itemTemplateData := ItemTemplateData{
			Item: item,
			Bid:  newBid,
		}

		// execute thanks template
		_ = tmpl.Execute(w, itemTemplateData)
		// Increment Prometheus Metric Counter
		itemReqs.WithLabelValues(item.Name, r.Method).Inc()

	} else {
		_, _ = fmt.Fprint(w, "Stop that")
	}
}

func adminHandler(w http.ResponseWriter, r *http.Request) {
	adminReqs.Inc()
	log.Printf("[INFO] Admin Request from %s", r.RemoteAddr)
	_, _ = fmt.Fprint(w, "Hello World! Admin Page")
}

func init() {
	_ = prometheus.Register(itemReqs)
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
	_ = db.AutoMigrate(&Item{})
	_ = db.AutoMigrate(&Bid{})
	log.Print("Migrating Tables Complete")

	firstItem := Item{
		Name:        "Nintendo Switch",
		Value:       300,
		ProvidedBy:  "Nintendo",
		Description: "A Nintendo Switch",
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
