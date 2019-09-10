# bds-xlm
## services/horizon/internal/init_ingester.go
Modify SetupServerArgs method to support some new commands about kafka related startup commands.

```
ingest.Config{
			EnableAssetStats: app.config.EnableAssetStats,
			Kafka:            app.config.Kafka,
			KafkaTopic:       app.config.KafkaTopic,
			KafkaProxyPort:   app.config.KafkaProxyPort,
			KafkaProxyHost:   app.config.KafkaProxyHost,
}
```

## services/horizon/internal/ingest/main.go

```
{
	Kafka            bool
	KafkaProxyHost   string
	KafkaProxyPort   uint
	KafkaTopic       string
}
```

## services/horizon/internal/db2/history/ledger.go

```
//load ledger
func (action *SendAction) LoadLedgerRecord() 

//load transaction
func (action *SendAction) LoadTransactionRecordByLedger() 

//load operation
func (action *SendAction) LoadOperationRecordByLedger() 
```

## add services/horizon/internal/actions_send.go
implement sendBlockToKafka method

```
//send data to kafka
func (action *SendAction) SendDataToKafka() {
}

// load block data
func (action *SendAction) LoadBlockData() {
}
```
