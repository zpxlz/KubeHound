package edge

import (
	"context"
	"fmt"

	"github.com/DataDog/KubeHound/pkg/kubehound/graph/adapter"
	"github.com/DataDog/KubeHound/pkg/kubehound/graph/types"
	"github.com/DataDog/KubeHound/pkg/kubehound/models/converter"
	"github.com/DataDog/KubeHound/pkg/kubehound/storage/cache"
	"github.com/DataDog/KubeHound/pkg/kubehound/storage/storedb"
	"github.com/DataDog/KubeHound/pkg/kubehound/store/collections"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

func init() {
	Register(TokenBruteforceNamespace{})
}

// @@DOCLINK: https://datadoghq.atlassian.net/wiki/spaces/ASE/pages/2887155994/TOKEN+BRUTEFORCE
type TokenBruteforceNamespace struct {
}

type tokenBruteforceNSGroup struct {
	Role     primitive.ObjectID `bson:"_id" json:"role"`
	Identity primitive.ObjectID `bson:"identity" json:"identity"`
}

func (e TokenBruteforceNamespace) Label() string {
	return "TOKEN_BRUTEFORCE"
}

func (e TokenBruteforceNamespace) Name() string {
	return "TokenBruteforceNamespace"
}

func (e TokenBruteforceNamespace) BatchSize() int {
	return BatchSizeDefault
}

func (e TokenBruteforceNamespace) Processor(ctx context.Context, oic *converter.ObjectIDConverter, entry any) (any, error) {
	typed, ok := entry.(*tokenBruteforceNSGroup)
	if !ok {
		return nil, fmt.Errorf("invalid type passed to processor: %T", entry)
	}

	return adapter.GremlinEdgeProcessor(ctx, oic, e.Label(), typed.Role, typed.Identity)
}

func (e TokenBruteforceNamespace) Traversal() types.EdgeTraversal {
	return adapter.DefaultEdgeTraversal()
}

// Stream finds all roles that are namespaced and have secrets/get or equivalent wildcard permissions and matching identities.
// Matching identities are defined as namespaced identities that share the role namespace or non-namespaced identities.
func (e TokenBruteforceNamespace) Stream(ctx context.Context, store storedb.Provider, _ cache.CacheReader,
	callback types.ProcessEntryCallback, complete types.CompleteQueryCallback) error {

	roles := adapter.MongoDB(store).Collection(collections.RoleName)
	pipeline := []bson.M{
		{
			"$match": bson.M{
				"is_namespaced": true,
				"rules": bson.M{
					"$elemMatch": bson.M{
						"$and": bson.A{
							bson.M{"$or": bson.A{
								bson.M{"resources": "secrets"},
								bson.M{"resources": "secrets/*"},
								bson.M{"resources": "*"},
							}},
							bson.M{"$or": bson.A{
								bson.M{"verbs": "get"},
								bson.M{"verbs": "*"},
							}},
						},
					},
				},
			},
		},
		{
			"$lookup": bson.M{
				"as":   "idsInNamespace",
				"from": "identities",
				"let": bson.M{
					"roleNamespace": "$namespace",
				},
				"pipeline": []bson.M{
					{
						"$match": bson.M{"$and": bson.A{
							bson.M{"$or": bson.A{
								bson.M{"$expr": bson.M{
									"$eq": bson.A{
										"$namespace", "$$roleNamespace",
									},
								}},
								bson.M{"is_namespaced": false},
							}},
							bson.M{"$or": bson.A{
								bson.M{"type": "ServiceAccount"},
								bson.M{"type": "User"},
							}},
						}},
					},
					{
						"$project": bson.M{
							"_id": 1,
						},
					},
				},
			},
		},
		{
			"$unwind": "$idsInNamespace",
		},
		{
			"$project": bson.M{
				"_id":      1,
				"identity": "$idsInNamespace._id",
			},
		},
	}

	cur, err := roles.Aggregate(context.Background(), pipeline)
	if err != nil {
		return err
	}
	defer cur.Close(ctx)

	return adapter.MongoCursorHandler[tokenBruteforceNSGroup](ctx, cur, callback, complete)
}