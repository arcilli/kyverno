package processor

import (
	"fmt"

	kyvernov1 "github.com/kyverno/kyverno/api/kyverno/v1"
	kyvernov1beta1 "github.com/kyverno/kyverno/api/kyverno/v1beta1"
	"github.com/kyverno/kyverno/cmd/cli/kubectl-kyverno/log"
	"github.com/kyverno/kyverno/cmd/cli/kubectl-kyverno/resource"
	"github.com/kyverno/kyverno/cmd/cli/kubectl-kyverno/store"
	"github.com/kyverno/kyverno/pkg/background/generate"
	"github.com/kyverno/kyverno/pkg/clients/dclient"
	"github.com/kyverno/kyverno/pkg/config"
	"github.com/kyverno/kyverno/pkg/engine"
	"github.com/kyverno/kyverno/pkg/engine/adapters"
	engineapi "github.com/kyverno/kyverno/pkg/engine/api"
	"github.com/kyverno/kyverno/pkg/engine/jmespath"
	"github.com/kyverno/kyverno/pkg/imageverifycache"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
)

func handleGeneratePolicy(generateResponse *engineapi.EngineResponse, policyContext engine.PolicyContext, ruleToCloneSourceResource map[string]string) ([]engineapi.RuleResponse, error) {
	newResource := policyContext.NewResource()
	objects := []runtime.Object{&newResource}
	resources := []*unstructured.Unstructured{}
	for _, rule := range generateResponse.PolicyResponse.Rules {
		if path, ok := ruleToCloneSourceResource[rule.Name()]; ok {
			resourceBytes, err := resource.GetFileBytes(path)
			if err != nil {
				fmt.Printf("failed to get resource bytes\n")
			} else {
				resources, err = resource.GetUnstructuredResources(resourceBytes)
				if err != nil {
					fmt.Printf("failed to convert resource bytes to unstructured format\n")
				}
			}
		}
	}

	for _, res := range resources {
		objects = append(objects, res)
	}

	c, err := initializeMockController(objects)
	if err != nil {
		fmt.Println("error at controller")
		return nil, err
	}

	gr := kyvernov1beta1.UpdateRequest{
		Spec: kyvernov1beta1.UpdateRequestSpec{
			Type:   kyvernov1beta1.Generate,
			Policy: generateResponse.Policy().GetName(),
			Resource: kyvernov1.ResourceSpec{
				Kind:       generateResponse.Resource.GetKind(),
				Namespace:  generateResponse.Resource.GetNamespace(),
				Name:       generateResponse.Resource.GetName(),
				APIVersion: generateResponse.Resource.GetAPIVersion(),
			},
		},
	}

	var newRuleResponse []engineapi.RuleResponse

	for _, rule := range generateResponse.PolicyResponse.Rules {
		genResource, err := c.ApplyGeneratePolicy(log.Log.V(2), &policyContext, gr, []string{rule.Name()})
		if err != nil {
			return nil, err
		}

		if genResource != nil {
			unstrGenResource, err := c.GetUnstrResource(genResource[0])
			if err != nil {
				return nil, err
			}
			newRuleResponse = append(newRuleResponse, *rule.WithGeneratedResource(*unstrGenResource))
		}
	}

	return newRuleResponse, nil
}

func initializeMockController(objects []runtime.Object) (*generate.GenerateController, error) {
	client, err := dclient.NewFakeClient(runtime.NewScheme(), nil, objects...)
	if err != nil {
		fmt.Printf("Failed to mock dynamic client")
		return nil, err
	}
	client.SetDiscovery(dclient.NewFakeDiscoveryClient(nil))
	cfg := config.NewDefaultConfiguration(false)
	c := generate.NewGenerateControllerWithOnlyClient(client, engine.NewEngine(
		cfg,
		config.NewDefaultMetricsConfiguration(),
		jmespath.New(cfg),
		adapters.Client(client),
		nil,
		imageverifycache.DisabledImageVerifyCache(),
		store.ContextLoaderFactory(nil),
		nil,
		"",
	))
	return c, nil
}
