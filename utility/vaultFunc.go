package utility

import (
	"encoding/json"
	"fmt"
	log "github.com/sirupsen/logrus"
	"net/http"
	"strings"
	"time"
	"vault-initializer/common"
)

// startInitializingWithIndividualPodIP starts the pod initializing on a targeted IP address
func startInitializingWithIndividualPodIP() {
	firstPodName, firstPodIP := getFirstResponsivePod()
	log.Infof("entering the initialization mode against pod name %s with podIP %s", firstPodName, firstPodIP)

	initPodURL := "http://" + strings.TrimSpace(firstPodIP) + ":8200/v1/sys/init"
	if !isInitializedOnPodIP(firstPodName, initPodURL) {
		initJSONString := fmt.Sprintf("{ \"secret_shares\": %d, \"secret_threshold\": %d }", common.SecShares, common.SecThreshold)
		initResponse, err := FireRequest(initJSONString, initPodURL, nil, common.HttpMethodPUT)
		if err != nil {
			log.Fatalf("Couldn't complete the init process, following error occurred: %v", err)
		}
		parseJSONRespo(initResponse, &parsedKeys)
		log.Debugf("The parsed Keys are %s ", parsedKeys)
		err = storeInSecret(parsedKeys)
		if err != nil {
			log.Fatalf("error while storing the init keys into k8s secrets: %v", err)
		}
	} else {
		log.Infof("Vault running on %s pod is already initialized", firstPodName)
	}
}

// configureLDAPInPod will start enabling the LDAP auth method and
// configures to the given LDAP servers
func configureLDAPInPod() {
	firstPodName, firstPodIP := getFirstResponsivePod()
	ldapEnablePodURL := "http://" + strings.TrimSpace(firstPodIP) + ":8200/v1/sys/auth/ldap"
	ldapConfigPodURL := "http://" + strings.TrimSpace(firstPodIP) + ":8200/v1/auth/ldap/config"
	_, err := FireRequest(enableLDAPJsonStr, ldapEnablePodURL, getAuthTokenHeaders(), common.HttpMethodPOST)
	if err != nil {
		log.Fatalf("Couldn't complete the ldap configuration process, following error occurred on %s pod: %v", firstPodName, err)
	} else {
		_, err := FireRequest(ldapConfigString, ldapConfigPodURL, getAuthTokenHeaders(), common.HttpMethodPUT)
		if err != nil {
			log.Fatalf("Couldn't complete the configuration of LDAP auth on %s pod, following error occurred: %v", firstPodName, err)
		}
	}
}

// enableSecretEngineInPod will enables the given secret engine in the configmap
func enableSecretEngineInPod() {
	firstPodName, firstPodIP := getFirstResponsivePod()
	secretEnginesPodURL := "http://" + strings.TrimSpace(firstPodIP) + ":8200/v1/sys/mounts/"
	if len(configMapObject.Data["secretEngines"]) > 0 {
		var secretEngineInterface map[string]map[string]interface{}
		json.Unmarshal([]byte(configMapObject.Data["secretEngines"]), &secretEngineInterface)
		for engineName, payloadJsonStr := range secretEngineInterface {
			byteArr, err := json.Marshal(payloadJsonStr)
			if err != nil {
				log.Fatalf("error while marshalling the payload for %s engine", engineName)
			} else {
				log.Debugf("proceeding to enable the engine path %s with the payload %s on the pod %s", engineName, string(byteArr), firstPodName)
				_, err := FireRequest(string(byteArr), secretEnginesPodURL+strings.TrimSpace(engineName), getAuthTokenHeaders(), common.HttpMethodPUT)
				if err != nil {
					log.Fatalf("error while enabling the secret engine to pod %s as %s", firstPodName, engineName)
				}
			}
		}
	}
}

// writePolicyInPod will extract the mapping for the policy to the user and/or group and binds it accordingly
func writePolicyInPod() {

	firstPodName, firstPodIP := getFirstResponsivePod()
	policyWritePodURL := "http://" + strings.TrimSpace(firstPodIP) + ":8200/v1/sys/policy/"
	ldapConfigGroupPolicyPodURL := "http://" + strings.TrimSpace(firstPodIP) + ":8200/v1/auth/ldap/groups/"
	ldapConfigUserPolicyPodURL := "http://" + strings.TrimSpace(firstPodIP) + ":8200/v1/auth/ldap/users/"

	for key, val := range configMapObject.Data {
		if strings.HasSuffix(key, ".hcl") {
			log.Infof("Proceeding to write the policy %s with the value: %v", key, val)
			byteDataArray, err := json.Marshal(val)
			jsonPayload := `{ "policy":` + string(byteDataArray) + `}`
			if err != nil {
				log.Errorf("error in marshalling the %s file for passing as payload", err)
			} else {
				response, err := FireRequest(jsonPayload, policyWritePodURL+strings.TrimSuffix(key, ".hcl"), getAuthTokenHeaders(), common.HttpMethodPUT)
				if err != nil {
					log.Errorf("error while creating %s policy on %s pod: %v ", key, firstPodName, err)
				}
				log.Debugf("response from write policy on the pod %s is: %v", firstPodName, response)
			}
		}
	}

	log.Infof("Policy upload done, proceeding to bind the policies.")
	if len(configMapObject.Data["ldapPolicyGroupMappings"]) > 0 {
		var policyMappingInterface map[string]map[string][]string
		err := json.Unmarshal([]byte(configMapObject.Data["ldapPolicyGroupMappings"]), &policyMappingInterface)
		if err != nil {
			log.Errorf("error while un-marshalling the policy mappings, err: %v", err)
		} else {
			readGroups := policyMappingInterface["groups"]["r_groups"]
			readWriteGroups := policyMappingInterface["groups"]["rw_groups"]
			readUsers := policyMappingInterface["groups"]["r_users"]
			readWriteUsers := policyMappingInterface["groups"]["rw_users"]

			readPolicies := policyMappingInterface["policies"]["r_policy"]
			readPolicyPayload := `{"policies":"` + strings.Join(readPolicies, ",") + `"}`
			readWritePolicies := policyMappingInterface["policies"]["rw_policy"]
			readWritePolicyPayload := `{"policies":"` + strings.Join(readWritePolicies, ",") + `"}`

			bindPolicy(readGroups, readPolicyPayload, ldapConfigGroupPolicyPodURL)
			bindPolicy(readWriteGroups, readWritePolicyPayload, ldapConfigGroupPolicyPodURL)
			bindPolicy(readUsers, readPolicyPayload, ldapConfigUserPolicyPodURL)
			bindPolicy(readWriteUsers, readWritePolicyPayload, ldapConfigUserPolicyPodURL)
		}
	} else {
		log.Info("No policy mappings found for the groups")
	}
}

// unsealIndividualPods starts unsealing the individual pods given their IP using the init keys
// if the error is "no host available", it means the pod is terminated. the control will go to re-populate the
// pod names and IP addresses for the same
func unsealIndividualPods(podName, podIP string) {
	podURL := "http://" + strings.TrimSpace(podIP) + ":8200"
	podUnsealURL := "http://" + strings.TrimSpace(podIP) + ":8200/v1/sys/unseal"

	for {
		res, err := http.Head(podURL)
		if err != nil {
			log.Errorf("error from unsealIndividualPods; service running in pod %s is not accepting the connection: %v waiting for %d seconds...", podName, err.Error(), common.WaitTime)
			// If the pod gets deleted and a new pod comes up, the loop should break and re-populate the IPs
			if strings.Contains(err.Error(), "connect: no route to host") {
				time.Sleep(5 * time.Second)
				populatePodNameKeysAndIPs()
				return
			}
			time.Sleep(time.Duration(common.WaitTimeSeconds) * time.Second)
		} else {
			log.Infof("Service is live with status %v", res.Status)
			break
		}
	}

	for sharesCount := 0; sharesCount < common.SecThreshold; sharesCount++ {
		jsonUnsealString := fmt.Sprintf("{\"key\": \"%s\"}", parsedKeys.Keys[sharesCount])
		responseBody, err := FireRequest(jsonUnsealString, podUnsealURL, nil, common.HttpMethodPUT)
		if err != nil {
			log.Fatalf("Couldn't complete the unseal process for the pod %s, following error occurred: %v", podName, err)
		}
		parseJSONRespo(responseBody, &unsealResponse)
	}
	if unsealResponse.Progress == 0 {
		log.Info("Unsealing for the pod %s is done.", podName)
	}
}

// isIndividualPodSealed returns a bool value for seal status on an individual pod given the IP address
func isIndividualPodSealed(podName, podIP string) bool {
	podURL := "http://" + strings.TrimSpace(podIP) + ":8200/v1/sys/seal-status"
	sealStatusResponse, err := FireRequest("", podURL, nil, common.HttpMethodGET)
	if err != nil {
		log.Errorf("error while checking the seal status on %s; err: %v", podURL, err)
		return true
	} else {
		log.Debugf("Seal response %v", string(sealStatusResponse))
		var responseJSON map[string]interface{}
		json.Unmarshal(sealStatusResponse, &responseJSON)
		if (responseJSON["sealed"]).(bool) {
			log.Infof("Vault is sealed on the pod %s", podName)
			return true
		} else {
			log.Infof("Vault is un-sealed on the pod %s", podName)
			return false
		}
	}
}
