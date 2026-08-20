package logctx

import hfl "github.com/openshift-hyperfleet/hyperfleet-logger"

var (
	SubsetKey         = hfl.NewKey[string]("subset")
	TopicKey          = hfl.NewKey[string]("topic")
	DecisionReasonKey = hfl.NewKey[string]("decision_reason")
)

func ContextFields() []hfl.ContextField {
	return []hfl.ContextField{
		hfl.StringField(SubsetKey),
		hfl.StringField(TopicKey),
		hfl.StringField(DecisionReasonKey),
	}
}
