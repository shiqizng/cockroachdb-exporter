package exporter

import (
	"context"
	_ "embed"
	"errors"
	"fmt"

	"github.com/algorand/conduit-plugin-template/plugin/exporter/idb"
	sdk "github.com/algorand/go-algorand-sdk/v2/types"
	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v2"

	"github.com/algorand/conduit/conduit/data"
	"github.com/algorand/conduit/conduit/plugins"
	"github.com/algorand/conduit/conduit/plugins/exporters"
)

//go:embed sample.yaml
var sampleConfig string

// metadata contains information about the plugin used for CLI helpers.
var metadata = plugins.Metadata{
	Name:         "cockroachdb",
	Description:  "CockroachDB exporter.",
	Deprecated:   false,
	SampleConfig: sampleConfig,
}

func init() {
	exporters.Register(metadata.Name, exporters.ExporterConstructorFunc(func() exporters.Exporter {
		return &cockroachdbExporter{}
	}))
}

type Config struct {
	ConnectionString string `yaml:"connection-string"`
}

// ExporterTemplate is the object which implements the exporter plugin interface.
type cockroachdbExporter struct {
	log    *logrus.Logger
	cfg    Config
	ctx    context.Context
	cf     context.CancelFunc
	logger *logrus.Logger
	db     idb.IndexerDb
	round  uint64
}

func (exp *cockroachdbExporter) Metadata() plugins.Metadata {
	return metadata
}

func (exp *cockroachdbExporter) Config() string {
	ret, _ := yaml.Marshal(exp.cfg)
	return string(ret)
}

func (exp *cockroachdbExporter) Close() error {
	return nil
}

// createIndexerDB common code for creating the IndexerDb instance.
func createIndexerDB(logger *logrus.Logger, readonly bool, cfg plugins.PluginConfig) (idb.IndexerDb, chan struct{}, error) {
	var eCfg Config
	if err := cfg.UnmarshalConfig(&eCfg); err != nil {
		return nil, nil, fmt.Errorf("connect failure in unmarshalConfig: %v", err)
	}

	// Inject a dummy db for unit testing
	dbName := "cockroachdb"
	var opts idb.IndexerDbOptions
	//opts.MaxConn = eCfg.MaxConn
	opts.ReadOnly = readonly

	//// for some reason when ConnectionString is empty, it's automatically
	//// connecting to a local instance that's running.
	//// this behavior can be reproduced in TestConnectDbFailure.
	//if !eCfg.Test && eCfg.ConnectionString == "" {
	//	return nil, nil, fmt.Errorf("connection string is empty for %s", dbName)
	//}
	db, ready, err := idb.IndexerDbByName(dbName, eCfg.ConnectionString, opts, logger)
	if err != nil {
		return nil, nil, fmt.Errorf("connect failure constructing db, %s: %v", dbName, err)
	}

	return db, ready, nil
}

func checkGenesisHash(db idb.IndexerDb, gh sdk.Digest) error {
	network, err := db.GetNetworkState()
	if errors.Is(err, idb.ErrorNotInitialized) {
		err = db.SetNetworkState(gh)
		if err != nil {
			return fmt.Errorf("error setting network state %w", err)
		}
		return nil
	} else if err != nil {
		return fmt.Errorf("unable to fetch network state from db %w", err)
	}
	if network.GenesisHash != gh {
		return fmt.Errorf("genesis hash not matching")
	}
	return nil
}

// EnsureInitialImport imports the genesis block if needed. Returns true if the initial import occurred.
func EnsureInitialImport(db idb.IndexerDb, genesis sdk.Genesis) (bool, error) {
	_, err := db.GetNextRoundToAccount()
	// Exit immediately or crash if we don't see ErrorNotInitialized.
	if err != idb.ErrorNotInitialized {
		if err != nil {
			return false, fmt.Errorf("getting import state, %v", err)
		}
		err = checkGenesisHash(db, genesis.Hash())
		if err != nil {
			return false, err
		}
		return false, nil
	}

	// Import genesis file from file or algod.
	err = db.LoadGenesis(genesis)
	if err != nil {
		return false, fmt.Errorf("could not load genesis json, %v", err)
	}
	return true, nil
}

func (exp *cockroachdbExporter) Init(ctx context.Context, ip data.InitProvider, cfg plugins.PluginConfig, logger *logrus.Logger) error {
	exp.ctx, exp.cf = context.WithCancel(ctx)
	exp.logger = logger

	db, ready, err := createIndexerDB(exp.logger, false, cfg)
	if err != nil {
		return fmt.Errorf("db create error: %v", err)
	}
	<-ready

	exp.db = db
	_, err = EnsureInitialImport(exp.db, *ip.GetGenesis())
	if err != nil {
		return fmt.Errorf("error importing genesis: %v", err)
	}
	dbRound, err := db.GetNextRoundToAccount()
	if err != nil {
		return fmt.Errorf("error getting next db round : %v", err)
	}
	if uint64(ip.NextDBRound()) != dbRound {
		return fmt.Errorf("initializing block round %d but next round to account is %d", ip.NextDBRound(), dbRound)
	}
	exp.round = uint64(ip.NextDBRound())

	return nil

}

func (exp *cockroachdbExporter) Receive(exportData data.BlockData) error {
	exp.log.Infof("Processing block %d", exportData.Round())

	// TODO: Your receive block data logic here.

	return nil
}
