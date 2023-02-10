package main

import (
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"github.com/gorilla/mux"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"html/template"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// NOTES:
// Got DB query working, but it still delivers an "empty" page on not-found IDs, need to fix
// Reject after time works, high bid works, entering bid works
// Basic auth works, decided to move some of admin stuff to external scripts, there's no real
// Need to have those functions in the admin panel
// TODO: Winners display page
//
//	Cards per bidder, list of items won, display as responsive grid
var (
	Event            string
	EndTimeString    string
	EndTime          time.Time
	ExpectedUsername string
	ExpectedPassword string
	DbUsername       string
	DbPassword       string
	DbDatabase       string
	DbHost           string
	DbPort           string
	db               *gorm.DB
	err              error
	tmpls            = template.Must(template.ParseGlob("static/templates/*.html"))
	indexReqs        = promauto.NewCounter(
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
)

// Borrowed with great appreciation from
// https://www.alexedwards.net/blog/basic-authentication-in-go
func (app *application) basicAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		username, password, ok := r.BasicAuth()
		if ok {
			usernameHash := sha256.Sum256([]byte(username))
			passwordHash := sha256.Sum256([]byte(password))
			ExpectedUsernameHash := sha256.Sum256([]byte(ExpectedUsername))
			ExpectedPasswordHash := sha256.Sum256([]byte(ExpectedPassword))

			usernameMatch := subtle.ConstantTimeCompare(usernameHash[:], ExpectedUsernameHash[:]) == 1
			passwordMatch := subtle.ConstantTimeCompare(passwordHash[:], ExpectedPasswordHash[:]) == 1

			if usernameMatch && passwordMatch {
				next.ServeHTTP(w, r)
				return
			}
		}
		w.Header().Set("WWW-Authenticate", `Basic realm="restricted", charset="UTF-8"`)
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
	}
}

func (item *Item) GetHighBid(bid *Bid) {
	db.Order("bid_amount desc").Find(bid, "item_id = ?", item.ID).Limit(1)
}

func (item *Item) GetAllBids(bids *[]Bid) {
	db.Order("bid_amount desc").Find(bids, "item_id = ?", item.ID)
}

func (item *Item) FoundMatchingBid(amount uint) bool {
	var bids []Bid
	db.Where("item_id = ? AND bid_amount = ?", item.ID, amount).Find(&bids)
	if len(bids) != 0 {
		return true
	}
	return false
}

func (item *Item) BidUnderMinBid(amount uint) bool {
	// if new bid amount is LESS THAN min bid, return true
	return float32(amount) < item.MinBid
}

func (item *Item) BidUnderHighBid(amount uint) bool {
	var bid Bid
	item.GetHighBid(&bid)
	return amount <= bid.BidAmount
}

func indexHandler(w http.ResponseWriter, r *http.Request) {
	tmplData := IndexTemplateData{Event: Event}
	err = tmpls.ExecuteTemplate(w, "index.html", tmplData)
	if err != nil {
		log.Printf("[ERROR] Execute Template Error line 90 - %v", err.Error())
	}
	indexReqs.Inc()
	log.Printf("[INFO] Hello World from %s", r.RemoteAddr)
}

func itemHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		item := Item{}
		vars := mux.Vars(r)
		var (
			bid      Bid
			valueStr string
		)

		itemID, err := strconv.ParseUint(vars["itemID"], 10, 32)
		if err != nil {
			w.WriteHeader(http.StatusNotFound)
			err = tmpls.ExecuteTemplate(w, "error.html", ErrorPageData{Message: "Invalid Item"})
			if err != nil {
				log.Printf("[ERROR] Execute Template Error line 107 - %v", err.Error())
			}
			log.Printf("Item ID was broken - %v", r.RequestURI)
			return
		}

		// Get Item From Database
		err = db.First(&item, itemID).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			w.WriteHeader(http.StatusNotFound)
			err = tmpls.ExecuteTemplate(w, "error.html", ErrorPageData{Message: "That item was not found"})
			if err != nil {
				log.Printf("[ERROR] Execute Template Error line 118 - %v", err.Error())
			}
			return
		}

		// Get High Bid From Database
		item.GetHighBid(&bid)

		// Pad values to 2 decimal places
		valueStr = fmt.Sprintf("%.2f", item.Value)
		// If the value is a whole dollar, trim the .00 cents
		valueStr = strings.TrimSuffix(valueStr, ".00")

		// Insert info into HTML Template
		itemTemplateData := ItemTemplateData{
			Item:     item,
			Bid:      bid,
			ValueStr: valueStr,
		}

		// Execute Template
		err = tmpls.ExecuteTemplate(w, "item.html", itemTemplateData)
		if err != nil {
			log.Printf("[ERROR] Execute Template Error line 135 - %v", err.Error())
		}

		// Increment Prometheus Metric Counter
		itemReqs.WithLabelValues(item.Name, r.Method).Inc()

		remoteAddr, _, _ := net.SplitHostPort(r.RemoteAddr)
		log.Printf("[INFO] Item %d requested by %s", item.ID, remoteAddr)

	case http.MethodPost:
		if time.Now().After(EndTime) {
			w.WriteHeader(http.StatusUnauthorized)
			err = tmpls.ExecuteTemplate(w, "error.html", ErrorPageData{Message: "I'm sorry, the auction has closed."})
			if err != nil {
				log.Printf("[ERROR] Execute Template Error line 159 - %v", err.Error())
			}
			log.Print("Bid after close time")
			return
		}

		// Get variables from the Mux router
		vars := mux.Vars(r)
		// Read the ItemID
		ItemID, err := strconv.ParseUint(vars["itemID"], 10, 32)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			err = tmpls.ExecuteTemplate(w, "error.html", ErrorPageData{Message: "Invalid Item"})
			if err != nil {
				log.Printf("[ERROR] Execute Template Error line 172 - %v", err.Error())
			}
			log.Print("Item ID was broken")
			return
		}

		// Read in items from form
		AuctionID, err := strconv.ParseUint(r.FormValue("AuctionID"), 10, 32)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			err = tmpls.ExecuteTemplate(w, "error.html", ErrorPageData{Message: "Auction ID must be a number"})
			if err != nil {
				log.Printf("[ERROR] Execute Template Error line 183 - %v", err.Error())
			}
			log.Print("Auction ID was not a number")
			return
		}
		BidAmount, err := strconv.ParseUint(r.FormValue("BidAmount"), 10, 32)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			err = tmpls.ExecuteTemplate(w, "error.html", ErrorPageData{Message: "Bid Amount must be a number"})
			if err != nil {
				log.Printf("[ERROR] Execute Template Error line 193 - %v", err.Error())
			}
			log.Print("Bid Amount was not a number")
			return
		}

		// Build a Bid Item from form data
		newBid := Bid{
			AuctionID: uint(AuctionID),
			BidAmount: uint(BidAmount),
			ItemID:    uint(ItemID),
		}

		// Get corresponding Item for thanks page
		item := Item{}
		db.First(&item, ItemID)

		// Ensure we're not allowing duplicate bids
		if item.FoundMatchingBid(newBid.BidAmount) {
			w.WriteHeader(http.StatusBadRequest)
			err = tmpls.ExecuteTemplate(w, "error.html", ErrorPageData{Message: "Duplicate bid, please refresh the page and try again"})
			if err != nil {
				log.Printf("[ERROR] Execute Template Error line 243 - %v", err.Error())
			}
			return
		}

		if item.BidUnderMinBid(newBid.BidAmount) {
			w.WriteHeader(http.StatusBadRequest)
			err = tmpls.ExecuteTemplate(w, "error.html", ErrorPageData{Message: "Bid amount under Minimum Bid, please refresh the page and try again"})
			if err != nil {
				log.Printf("[ERROR] Execute Template Error line 252 - %v", err.Error())
			}
			return
		}

		if item.BidUnderHighBid(newBid.BidAmount) {
			w.WriteHeader(http.StatusBadRequest)
			err = tmpls.ExecuteTemplate(w, "error.html", ErrorPageData{Message: "Bid amount under Current High Bid, please refresh the page and try again"})
			if err != nil {
				log.Printf("[ERROR] Execute Template Error line 252 - %v", err.Error())
			}
			return
		}

		db.Create(&newBid)

		// Build message for thanks page
		itemTemplateData := ItemTemplateData{
			Item:  item,
			Bid:   newBid,
			Event: Event,
		}

		// execute thanks template
		err = tmpls.ExecuteTemplate(w, "thanks.html", itemTemplateData)
		if err != nil {
			log.Printf("[ERROR] Execute Template Error line 220 - %v", err.Error())
		}
		// Increment Prometheus Metric Counter
		itemReqs.WithLabelValues(item.Name, r.Method).Inc()
		return

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
		_, _ = fmt.Fprint(w, "Stop that")
	}
}

func adminHandler(w http.ResponseWriter, r *http.Request) {
	// Get all items and
	var (
		items             []Item
		bids              []Bid
		adminTemplateData AdminTemplateData
		adminItem         ItemTemplateData
	)
	wonItemsTotals := make(map[uint]uint)

	adminTemplateData.Event = Event

	db.Find(&items)

	for _, item := range items {
		item.GetAllBids(&bids)
		if len(bids) == 0 {
			// If we have no bids, set up null values for zeros
			adminItem.Bid = Bid{}
		} else {
			adminItem.Bid = bids[0]
			_, present := wonItemsTotals[adminItem.Bid.AuctionID]
			if present {
				wonItemsTotals[adminItem.Bid.AuctionID] += adminItem.Bid.BidAmount
			} else {
				wonItemsTotals[adminItem.Bid.AuctionID] = adminItem.Bid.BidAmount
			}
		}
		adminItem.Item = item
		adminTemplateData.Items = append(adminTemplateData.Items, adminItem)
	}
	adminTemplateData.WonItemTotals = wonItemsTotals

	err = tmpls.ExecuteTemplate(w, "admin.html", adminTemplateData)
	if err != nil {
		log.Printf("[ERROR] Execute Template Error line 265 - %v", err.Error())
	}

	adminReqs.Inc()
	log.Printf("[INFO] Admin Request from %s", r.RemoteAddr)
}

func winnersHandler(w http.ResponseWriter, r *http.Request) {
	_ = r
	var (
		WinnersData WinnersTemplateData
		items       []Item
	)
	winners := make(map[uint][]string)

	WinnersData.Event = Event

	// Get all items
	db.Find(&items)
	for _, item := range items {
		var bid Bid
		item.GetHighBid(&bid)
		if bid.ID == 0 {
			continue
		}
		_, present := winners[bid.AuctionID]
		if present {
			winners[bid.AuctionID] = append(winners[bid.AuctionID], item.Name)
		} else {
			winners[bid.AuctionID] = []string{item.Name}
		}
	}

	winnersString := make(map[uint]string)
	for id, items := range winners {
		winnersString[id] = strings.Join(items, ", ")
	}

	WinnersData.Winners = winnersString

	err = tmpls.ExecuteTemplate(w, "winners.html", WinnersData)
	if err != nil {
		log.Printf("[ERROR] Execute Template Error line 348 - %v", err.Error())
	}
}

func init() {
	_ = prometheus.Register(itemReqs)
	Event = os.Getenv("AUCTION_EVENT")
	EndTimeString = os.Getenv("AUCTION_END_TIME")
	ExpectedUsername = os.Getenv("AUCTION_USER")
	ExpectedPassword = os.Getenv("AUCTION_PASS")
	DbUsername = os.Getenv("AUCTION_DB_USER")
	DbPassword = os.Getenv("AUCTION_DB_PASS")
	DbDatabase = os.Getenv("AUCTION_DB_DB")
	DbHost = os.Getenv("AUCTION_DB_HOST")
	DbPort = os.Getenv("AUCTION_DB_PORT")
	if Event == "" {
		log.Fatal("[FATAL] Event environment variable not set.")
	}
	if EndTimeString == "" {
		log.Fatal("[FATAL] End Time environment variable not set.")
	}
	if ExpectedUsername == "" {
		log.Fatal("[FATAL] Username environment variable not set.")
	}
	if ExpectedPassword == "" {
		log.Fatal("[FATAL] Password environment variable not set.")
	}
	if DbUsername == "" {
		log.Fatal("[FATAL] Database Username not set.")
	}
	if DbPassword == "" {
		log.Fatal("[FATAL] Database Password not set.")
	}
	if DbDatabase == "" {
		log.Fatal("[FATAL] Database Name not set.")
	}
	if DbHost == "" {
		log.Fatal("[FATAL] Database Host not set.")
	}
	if DbPort == "" {
		log.Fatal("[FATAL] Database Port not set.")
	}
	log.Printf("[INFO] Username and Password set - %s", ExpectedUsername)
}

func main() {
	app := new(application)
	app.auth.username = ExpectedUsername
	app.auth.password = ExpectedPassword
	css := http.FileServer(http.Dir("static/css"))
	log.Print("Setting up Database Connection")
	dsn := fmt.Sprintf(
		"%s:%s@tcp(%s:%s)/%s?charset=utf8mb4&parseTime=True&loc=Local",
		DbUsername,
		DbPassword,
		DbHost,
		DbPort,
		DbDatabase,
	)
	db, err = gorm.Open(mysql.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatal("[FATAL] Failed to connect to database")
	}
	log.Print("Database Connection Successful")

	log.Print("Parsing End Time")
	EndTime, err = time.Parse(time.RFC822, EndTimeString)
	if err != nil {
		log.Fatal(err)
	}

	log.Print("Migrating Tables Beginning")
	_ = db.AutoMigrate(&Item{})
	_ = db.AutoMigrate(&Bid{})
	log.Print("Migrating Tables Complete")

	router := mux.NewRouter()
	router.HandleFunc("/", indexHandler).Methods("GET")
	router.PathPrefix("/css/").Handler(http.StripPrefix("/css", css))
	router.HandleFunc("/admin", app.basicAuth(adminHandler)).Methods("GET", "POST")
	router.HandleFunc("/winners", app.basicAuth(winnersHandler)).Methods("GET")
	router.Handle("/metrics", promhttp.Handler())
	router.HandleFunc(fmt.Sprintf("/%s/{itemID}", Event), itemHandler).Methods("GET", "POST")

	log.Fatal(http.ListenAndServe(":8000", router))
}
