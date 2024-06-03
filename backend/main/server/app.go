package server

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/axiomzen/envconfig"
	"github.com/gorilla/mux"
	"github.com/jackc/pgx/v4/pgxpool"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/DapperCollectives/CAST/backend/main/middleware"
	"github.com/DapperCollectives/CAST/backend/main/models"
	"github.com/DapperCollectives/CAST/backend/main/shared"
	"github.com/DapperCollectives/CAST/backend/main/strategies"
)

type (
	Database             shared.Database
	IpfsClient           shared.IpfsClient
	Allowlist            shared.Allowlist
	Vote                 models.Vote
	Proposal             models.Proposal
	Community            models.Community
	Balance              models.Balance
	List                 models.List
	ListRequest          models.ListPayload
	ListUpdatePayload    models.ListUpdatePayload
	CommunityUser        models.CommunityUser
	CommunityUserPayload models.CommunityUserPayload
	UserCommunity        models.UserCommunity
)

type TxOptionsAddresses []string

type App struct {
	Router      *mux.Router
	DB          *shared.Database
	IpfsClient  *shared.IpfsClient
	FlowAdapter *shared.FlowAdapter

	TxOptionsAddresses []string
	Env                string
	AdminAllowlist     shared.Allowlist
	CommunityBlocklist shared.Allowlist
	Config             shared.Config
}

type Strategy interface {
	TallyVotes(
		votes []*models.VoteWithBalance,
		p *models.ProposalResults,
		proposal *models.Proposal,
	) (models.ProposalResults, error)
	GetVotes(
		votes []*models.VoteWithBalance,
		proposal *models.Proposal,
	) ([]*models.VoteWithBalance, error)
	GetVoteWeightForBalance(
		vote *models.VoteWithBalance,
		proposal *models.Proposal,
	) (float64, error)
	InitStrategy(f *shared.FlowAdapter, db *shared.Database)
	FetchBalance(b *models.Balance, p *models.Proposal) (*models.Balance, error)
	RequiresSnapshot() bool
}

var strategyMap = map[string]Strategy{
	"token-weighted-default":        &strategies.TokenWeightedDefault{},
	"total-token-weighted-default":  &strategies.TotalTokenWeightedDefault{},
	"staked-token-weighted-default": &strategies.StakedTokenWeightedDefault{},
	"one-address-one-vote":          &strategies.OneAddressOneVote{},
	"balance-of-nfts":               &strategies.BalanceOfNfts{},
	"float-nfts":                    &strategies.FloatNFTs{},
	"custom-script":                 &strategies.CustomScript{},
}

var customScripts []shared.CustomScript

var helpers Helpers

//////////////////////
// INSTANCE METHODS //
//////////////////////

func (a *App) Initialize() {
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stdout})

	// Env
	env := os.Getenv("APP_ENV")
	if flag.Lookup("test.v") != nil {
		env = "TEST"
		os.Setenv("APP_ENV", "TEST")
	} else if len(env) == 0 {
		env = "PROD"
	}
	a.Env = strings.TrimSpace(env)

	// Set log level based on env
	if a.Env == "PROD" {
		log.Logger = log.Logger.Level(zerolog.InfoLevel)
		log.Info().Msgf("Log level: %s for APP_ENV=%s", "INFO", a.Env)
	} else {
		log.Logger = log.Logger.Level(zerolog.DebugLevel)
		log.Info().Msgf("Log level: %s for APP_ENV=%s", "DEBUG", a.Env)
	}

	// Set App-wide Config
	err := envconfig.Process("FVT", &a.Config)
	if err != nil {
		log.Error().Err(err).Msg("Error Reading Configuration.")
		os.Exit(1)
	}

	////////////
	// Clients
	////////////

	// when running "make proposals" sets db to dev not test
	arg := flag.String("db", "", "database type")
	flag.Int("port", 5001, "port")
	flag.Int("amount", 4, "Amount of proposals to create")

	flag.Parse()
	if *arg == "local" {
		os.Setenv("APP_ENV", "DEV")
	}

	// Postgres
	dbname := os.Getenv("DB_NAME")

	// IPFS
	if os.Getenv("APP_ENV") == "TEST" || os.Getenv("APP_ENV") == "DEV" {
		flag.Bool("ipfs-override", true, "overrides ipfs call")
	} else {
		flag.Bool("ipfs-override", false, "overrides ipfs call")
	}

	// TEST Env
	if os.Getenv("APP_ENV") == "TEST" {
		dbname = os.Getenv("TEST_DB_NAME")
	}

	// Postgres
	a.ConnectDB(
		os.Getenv("DB_USERNAME"),
		os.Getenv("DB_PASSWORD"),
		os.Getenv("DB_HOST"),
		os.Getenv("DB_PORT"),
		dbname,
	)

	// IPFS
	a.IpfsClient = shared.NewIpfsClient(os.Getenv("IPFS_KEY"), os.Getenv("IPFS_SECRET"))

	// Flow

	// Load custom scripts for strategies
	scripts, err := ioutil.ReadFile("./main/cadence/scripts/custom/scripts.json")
	if err != nil {
		log.Error().Err(err).Msg("Error Reading Custom Strategy scripts.")
	}

	err = json.Unmarshal(scripts, &customScripts)
	if err != nil {
		log.Error().Err(err).Msg("Error during Unmarshalling custom scripts")
	}

	// Create Map for Flow Adaptor to look up when voting
	customScriptsMap := make(map[string]shared.CustomScript)
	for _, script := range customScripts {
		customScriptsMap[script.Key] = script
	}

	if os.Getenv("FLOW_ENV") == "" {
		os.Setenv("FLOW_ENV", "emulator")
	}
	a.FlowAdapter = shared.NewFlowClient(os.Getenv("FLOW_ENV"), customScriptsMap)

	// Snapshot
	a.TxOptionsAddresses = strings.Fields(os.Getenv("TX_OPTIONS_ADDRS"))

	// Router
	a.Router = mux.NewRouter()
	a.initializeRoutes()

	// Middlewares
	a.Router.Use(mux.CORSMethodMiddleware(a.Router))
	a.Router.Use(middleware.Logger)
	a.Router.Use(middleware.UseCors(a.Config))

	helpers.Initialize(a)
}

func (a *App) Run() {
	addr := fmt.Sprintf(":%s", os.Getenv("API_PORT"))
	log.Info().Msgf("Starting server on %s ...", addr)
	log.Fatal().Err(http.ListenAndServe(addr, a.Router)).Msgf("Server at %s crashed!", addr)
}

func (a *App) ConnectDB(username, password, host, port, dbname string) {
	var database shared.Database
	var err error

	database.Context = context.Background()
	database.Name = dbname

	connectionString := ToUnixURL(false, username, password, dbname, host)

	pconf, confErr := pgxpool.ParseConfig(connectionString)
	if confErr != nil {
		log.Fatal().Err(err).Msg("Unable to parse database config url")
	}

	if os.Getenv("APP_ENV") == "TEST" {
		log.Info().Msg("Setting MIN/MAX connections to 1")
		pconf.MinConns = 1
		pconf.MaxConns = 1
	}

	database.Conn, err = pgxpool.ConnectConfig(database.Context, pconf)

	database.Env = &a.Env
	if err != nil {
		log.Fatal().Err(err).Msg("Error creating Postsgres conn pool")
	} else {
		a.DB = &database
		log.Info().Msgf("Successfully created Postgres conn pool")
	}
}

func ToUnixURL(ssl bool, username, password, db, host string) string {
	urlStr := "postgresql://"

	if len(username) == 0 {
		username = "postgres"
	}
	urlStr += username

	if len(password) > 0 {
		urlStr = urlStr + ":" + url.PathEscape(password)
	}
	urlStr += "@"

	urlStr += "/" + url.PathEscape(db)

	// Append query parameters
	urlStr += "?" + "host=" + host

	mode := ""
	if !ssl {
		mode = "&sslmode=disable"
	}

	urlStr += mode
	return urlStr
}
