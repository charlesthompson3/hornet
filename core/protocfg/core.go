package protocfg

import (
	"encoding/json"

	flag "github.com/spf13/pflag"
	"go.uber.org/dig"

	"github.com/gohornet/hornet/pkg/keymanager"
	"github.com/gohornet/hornet/pkg/model/milestone"
	"github.com/iotaledger/hive.go/app"
	"github.com/iotaledger/hive.go/crypto"
	iotago "github.com/iotaledger/iota.go/v3"
)

func init() {
	_ = flag.CommandLine.MarkHidden(CfgProtocolPublicKeyRangesJSON)

	CoreComponent = &app.CoreComponent{
		Component: &app.Component{
			Name:           "ProtoCfg",
			Params:         params,
			InitConfigPars: initConfigPars,
		},
	}
}

var (
	CoreComponent *app.CoreComponent

	cooPubKeyRangesFlag = flag.String(CfgProtocolPublicKeyRangesJSON, "", "overwrite public key ranges (JSON)")
)

func initConfigPars(c *dig.Container) error {

	type cfgResult struct {
		dig.Out
		KeyManager              *keymanager.KeyManager
		MilestonePublicKeyCount int `name:"milestonePublicKeyCount"`
		ProtocolParameters      *iotago.ProtocolParameters
		BaseToken               *BaseToken
	}

	if err := c.Provide(func() cfgResult {

		res := cfgResult{
			MilestonePublicKeyCount: ParamsProtocol.MilestonePublicKeyCount,

			ProtocolParameters: &iotago.ProtocolParameters{
				Version:       ParamsProtocol.Parameters.Version,
				NetworkName:   ParamsProtocol.Parameters.NetworkName,
				Bech32HRP:     iotago.NetworkPrefix(ParamsProtocol.Parameters.Bech32HRP),
				MinPoWScore:   ParamsProtocol.Parameters.MinPoWScore,
				BelowMaxDepth: ParamsProtocol.Parameters.BelowMaxDepth,
				RentStructure: iotago.RentStructure{
					VByteCost:    ParamsProtocol.Parameters.RentStructureVByteCost,
					VBFactorData: iotago.VByteCostFactor(ParamsProtocol.Parameters.RentStructureVByteFactorData),
					VBFactorKey:  iotago.VByteCostFactor(ParamsProtocol.Parameters.RentStructureVByteFactorKey),
				},
				TokenSupply: ParamsProtocol.Parameters.TokenSupply,
			},
			BaseToken: &BaseToken{
				Name:            ParamsProtocol.BaseToken.Name,
				TickerSymbol:    ParamsProtocol.BaseToken.TickerSymbol,
				Unit:            ParamsProtocol.BaseToken.Unit,
				Subunit:         ParamsProtocol.BaseToken.Subunit,
				Decimals:        ParamsProtocol.BaseToken.Decimals,
				UseMetricPrefix: ParamsProtocol.BaseToken.UseMetricPrefix,
			},
		}

		// load from config
		keyRanges := ParamsProtocol.PublicKeyRanges
		if *cooPubKeyRangesFlag != "" {
			// load from special CLI flag
			if err := json.Unmarshal([]byte(*cooPubKeyRangesFlag), &keyRanges); err != nil {
				CoreComponent.LogPanic(err)
			}
		}

		keyManager, err := KeyManagerWithConfigPublicKeyRanges(keyRanges)
		if err != nil {
			CoreComponent.LogPanicf("can't load public key ranges: %s", err)
		}
		res.KeyManager = keyManager
		return res
	}); err != nil {
		CoreComponent.LogPanic(err)
	}

	return nil
}

func KeyManagerWithConfigPublicKeyRanges(coordinatorPublicKeyRanges ConfigPublicKeyRanges) (*keymanager.KeyManager, error) {
	keyManager := keymanager.New()
	for _, keyRange := range coordinatorPublicKeyRanges {
		pubKey, err := crypto.ParseEd25519PublicKeyFromString(keyRange.Key)
		if err != nil {
			return nil, err
		}

		keyManager.AddKeyRange(pubKey, milestone.Index(keyRange.StartIndex), milestone.Index(keyRange.EndIndex))
	}

	return keyManager, nil
}
