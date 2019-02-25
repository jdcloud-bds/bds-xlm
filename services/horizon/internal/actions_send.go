package horizon

import (
	"encoding/json"
	"fmt"
	"github.com/stellar/go/amount"
	"github.com/stellar/go/protocols/horizon/operations"
	"github.com/stellar/go/services/horizon/internal/db2/history"
	"github.com/stellar/go/services/horizon/internal/ledger"
	"github.com/stellar/go/services/horizon/internal/render/problem"
	"github.com/stellar/go/support/log"
	"github.com/stellar/go/support/render/hal"
	"github.com/stellar/go/xdr"
	"io/ioutil"
	"net/http"
	"strings"
	"time"
)

type ledgerInBlock struct {
	ID       string `json:"id"`
	PT       string `json:"paging_token"`
	Hash     string `json:"hash"`
	PrevHash string `json:"prev_hash,omitempty"`
	Sequence int32  `json:"sequence"`
	// Deprecated - remove in: horizon-v0.17.0
	TransactionCount           int32          `json:"transaction_count"`
	SuccessfulTransactionCount int32          `json:"successful_transaction_count"`
	FailedTransactionCount     *int32         `json:"failed_transaction_count"`
	OperationCount             int32          `json:"operation_count"`
	ClosedAt                   time.Time      `json:"closed_at"`
	TotalCoins                 string         `json:"total_coins"`
	FeePool                    string         `json:"fee_pool"`
	BaseFee                    int32          `json:"base_fee_in_stroops"`
	BaseReserve                int32          `json:"base_reserve_in_stroops"`
	MaxTxSetSize               int32          `json:"max_tx_set_size"`
	ProtocolVersion            int32          `json:"protocol_version"`
	HeaderXDR                  string         `json:"header_xdr"`
	Transactions               []*transaction `json:"transactions"`
}

type transaction struct {
	ID              string       `json:"id"`
	PT              string       `json:"paging_token"`
	Hash            string       `json:"hash"`
	Ledger          int32        `json:"ledger"`
	Account         string       `json:"source_account"`
	AccountSequence string       `json:"source_account_sequence"`
	FeePaid         int32        `json:"fee_paid"`
	OperationCount  int32        `json:"operation_count"`
	EnvelopeXdr     string       `json:"envelope_xdr"`
	ResultXdr       string       `json:"result_xdr"`
	ResultMetaXdr   string       `json:"result_meta_xdr"`
	FeeMetaXdr      string       `json:"fee_meta_xdr"`
	MemoType        string       `json:"memo_type"`
	Signatures      string       `json:"signatures"`
	Operations      []*operation `json:"operations"`
}

type operation struct {
	OperationID      string `json:"operation_id"`
	TransactionID    string `json:"transaction_id"`
	ApplicationOrder int32  `json:"application_order"`
	Type             string `json:"type"`
	Detail           string `json:"detail"`
	SourceAccount    string `json:"source_account"`
}

type BlockData struct {
	Records []BlockValue `json:"records"`
}

type BlockValue struct {
	Value ledgerInBlock `json:"value"`
}

// SendAction submits blocks to the kafka
type SendAction struct {
	Action
	SequenceStart      int32
	SequenceEnd        int32
	SendKafka          string
	LedgerRecords      []history.Ledger
	TransactionRecords []history.Transaction
	OperationRecords   []history.Operation
	BlockRecords       BlockData
	KafkaReply         Reply
}

type Reply struct {
	Message string `json:"message"`
}

// JSON is a method for actions.JSON
func (action *SendAction) JSON() error {
	action.Do(
		action.EnsureHistoryFreshness,
		action.loadParams,
		action.verifyWithinHistory,
		action.LoadLedgerRecord,
		action.LoadTransactionRecordByLedger,
		action.LoadOperationRecordByLedger,
		action.LoadBlockData,
		func() {
			if action.SendKafka == "true" {
				action.SendDataToKafka()
				hal.Render(action.W, action.KafkaReply)
			} else {
				hal.Render(action.W, action.BlockRecords)
			}
		},
	)
	return action.Err
}

func (action *SendAction) loadParams() {
	action.SequenceStart = action.GetInt32("ledger_start")
	action.SequenceEnd = action.GetInt32("ledger_end")
	action.SendKafka = action.GetString("send_kafka")
}

func (action *SendAction) LoadLedgerRecord() {
	action.Err = action.HistoryQ().LedgerBySequenceBatch(&action.LedgerRecords, action.SequenceStart, action.SequenceEnd)
}

func (action *SendAction) LoadTransactionRecordByLedger() {
	action.Err = action.HistoryQ().Transactions().ForLedgerBatch(action.SequenceStart, action.SequenceEnd).Select(&action.TransactionRecords)
}

func (action *SendAction) LoadOperationRecordByLedger() {
	action.Err = action.HistoryQ().Operations().ForLedgerBatch(action.SequenceStart, action.SequenceEnd).Select(&action.OperationRecords)
}

func (action *SendAction) verifyWithinHistory() {
	if action.SequenceStart < ledger.CurrentState().HistoryElder {
		action.Err = &problem.BeforeHistory
	}
}

func (action *SendAction) LoadBlockData() {
	ltMap := make(map[int32][]*transaction, 0)
	toMap := make(map[int64][]*operation, 0)
	action.BlockRecords.Records = make([]BlockValue, 0)
	for _, operationRecord := range action.OperationRecords {
		var op operation
		op.Type, _ = operations.TypeNames[operationRecord.Type]
		op.OperationID = fmt.Sprintf("%d", operationRecord.ID)
		op.TransactionID = fmt.Sprintf("%d", operationRecord.TransactionID)
		op.ApplicationOrder = operationRecord.ApplicationOrder
		op.Detail = operationRecord.DetailsString.String
		op.SourceAccount = operationRecord.SourceAccount
		_, exist := toMap[operationRecord.TransactionID]
		if !exist {
			toMap[operationRecord.TransactionID] = make([]*operation, 0)
		}
		toMap[operationRecord.TransactionID] = append(toMap[operationRecord.TransactionID], &op)
	}

	for _, transactionRecord := range action.TransactionRecords {
		var tr transaction
		tr.ID = fmt.Sprintf("%d", transactionRecord.ID)
		tr.PT = transactionRecord.PagingToken()
		tr.Hash = transactionRecord.TransactionHash
		tr.Ledger = transactionRecord.LedgerSequence
		tr.Account = transactionRecord.Account
		tr.AccountSequence = transactionRecord.AccountSequence
		tr.FeePaid = transactionRecord.FeeCharged
		tr.OperationCount = transactionRecord.OperationCount
		tr.EnvelopeXdr = transactionRecord.TxEnvelope
		tr.ResultXdr = transactionRecord.TxResult
		tr.ResultMetaXdr = transactionRecord.TxMeta
		tr.FeeMetaXdr = transactionRecord.TxFeeMeta
		tr.MemoType = transactionRecord.MemoType
		tr.Signatures = transactionRecord.SignatureString
		tr.Operations = make([]*operation, 0)
		_, exist := toMap[transactionRecord.ID]
		if exist {
			tr.Operations = toMap[transactionRecord.ID]
		}
		_, exist1 := ltMap[transactionRecord.LedgerSequence]
		if !exist1 {
			ltMap[transactionRecord.LedgerSequence] = make([]*transaction, 0)
		}
		ltMap[transactionRecord.LedgerSequence] = append(ltMap[transactionRecord.LedgerSequence], &tr)
	}

	for _, record := range action.LedgerRecords {
		var dest ledgerInBlock
		dest.ID = fmt.Sprintf("%d", record.ID)
		dest.PT = record.PagingToken()
		dest.Hash = record.LedgerHash
		dest.PrevHash = record.PreviousLedgerHash.String
		dest.Sequence = record.Sequence
		dest.TransactionCount = record.TransactionCount
		// Default to `transaction_count`
		dest.SuccessfulTransactionCount = record.TransactionCount
		if record.SuccessfulTransactionCount != nil {
			dest.SuccessfulTransactionCount = *record.SuccessfulTransactionCount
		}
		dest.FailedTransactionCount = record.FailedTransactionCount
		dest.OperationCount = record.OperationCount
		dest.ClosedAt = record.ClosedAt
		dest.TotalCoins = amount.String(xdr.Int64(record.TotalCoins))
		dest.FeePool = amount.String(xdr.Int64(record.FeePool))
		dest.BaseFee = record.BaseFee
		dest.BaseReserve = record.BaseReserve
		dest.MaxTxSetSize = record.MaxTxSetSize
		dest.ProtocolVersion = record.ProtocolVersion
		if record.LedgerHeaderXDR.Valid {
			dest.HeaderXDR = record.LedgerHeaderXDR.String
		} else {
			dest.HeaderXDR = ""
		}
		dest.Transactions = make([]*transaction, 0)
		_, exist := ltMap[record.Sequence]
		if exist {
			dest.Transactions = ltMap[record.Sequence]
		}
		action.BlockRecords.Records = append(action.BlockRecords.Records, BlockValue{Value: dest})
	}
	return
}

func (action *SendAction) SendDataToKafka() {
	sub, _ := json.Marshal(action.BlockRecords)
	body := ioutil.NopCloser(strings.NewReader(string(sub)))

	client := &http.Client{}
	var url string
	if action.App.config.KafkaProxyPort == uint(80) {
		url = fmt.Sprintf("http://%s/topics/%s", action.App.config.KafkaProxyHost, action.App.config.KafkaTopic)
	} else {
		url = fmt.Sprintf("http://%s:%d/topics/%s", action.App.config.KafkaProxyHost, action.App.config.KafkaProxyPort, action.App.config.KafkaTopic)
	}
	req, _ := http.NewRequest("POST", url, body)
	req.Header.Set("Content-Type", "application/vnd.kafka.json.v1+json")
	req.Header.Set("Host", action.App.config.KafkaProxyHost)

	resp, err := client.Do(req)
	defer resp.Body.Close()

	if err != nil {
		action.Err = err
		return
	}
	_, err = ioutil.ReadAll(resp.Body)
	if err != nil {
		action.Err = err
		return
	}
	action.KafkaReply.Message = "success"
	log.Info("Horizon send data into kafka successfully! ")
}
