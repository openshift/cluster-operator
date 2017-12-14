package ansible

import (
	"fmt"
	"testing"

	coapi "github.com/openshift/cluster-operator/pkg/apis/clusteroperator/v1alpha1"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestVarsGenerator(t *testing.T) {
	cluster := &coapi.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "dgoodwin-dev-1",
		},
	}
	g := NewVarsGenerator(cluster)

	expectedVarPairs := map[string]string{
		cloudProviderKindVar: "aws",
		awsClusterIDVar:      cluster.Name,
		elbBasenameVar:       cluster.Name,
		awsVPCNameVar:        cluster.Name,
		awsRegionVar:         "us-east-1",
		awsAMIVar:            fmt.Sprintf("%s-ami-base", cluster.Name),
	}

	varsFileContents, err := g.GenerateVars()
	if assert.NoError(t, err) {
		for k, v := range expectedVarPairs {
			assert.Equal(t, v, g.Vars[k])
			// Make sure our vars all appear in the file contents as expected:
			assert.Contains(t, varsFileContents, fmt.Sprintf("%s: %s\n", k, v))
		}
	}
}
