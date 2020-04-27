package utility

import (
	"encoding/json"
	"fmt"
	discovery "github.com/gkarthiks/k8s-discovery"
	log "github.com/sirupsen/logrus"
	v1 "k8s.io/api/core/v1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"net/http"
	"strconv"
	"strings"
	"time"
	"vault-initializer/common"
)

var (
	k8s                                            *discovery.K8s
	enableLDAPJsonStr, ldapConfigString, namespace string
	vaultLabelSelectors                            string
	parsedKeys                                     common.VaultInitResp
	unsealResponse                                 common.VaultUnsealResp
	configMapObject                                *v1.ConfigMap
	err                                            error
	podNameToIPMap                                 map[string]string
)

func init() {
	podNameToIPMap = make(map[string]string)
	k8s, _ = discovery.NewK8s()
	namespace, _ = k8s.GetNamespace()
	version, _ := k8s.GetVersion()
	log.Infof("Specified Namespace: %s ", namespace)
	log.Infof("Version of running Kubernetes: %s ", version)
}

// Parses the config map initially and extracts the values for initialization
func ParseInitConfigData(vaultInitConfigMap string) {
	configMapObject, err = k8s.Clientset.CoreV1().ConfigMaps(namespace).Get(vaultInitConfigMap, metaV1.GetOptions{})
	if err != nil {
		log.Fatalf("error while accessing the initialization configuration settings from configmap %s in %s namespace", vaultInitConfigMap, namespace)
	} else {
		log.Debugf("Obtained ConfigMap data %v ", configMapObject.Data)
		// Vault pod labels
		if len(configMapObject.Data["vaultLabelSelector"]) > 0 {
			vaultLabelSelectors = configMapObject.Data["vaultLabelSelector"]
		} else {
			errMessage := `vaultLabelSelector: 'app.kubernetes.io/name=vault,component=server'`
			log.Panicf("Vault pod label selectors are not set, cannot continue. Please add an object as %s", errMessage)
		}

		//	Secret Shares
		if len(configMapObject.Data["secretShares"]) > 0 {
			common.SecShares, err = strconv.Atoi(strings.TrimSpace(configMapObject.Data["secretShares"]))
			if err != nil {
				log.Warnf("error while parsing the secret shares input, defaulting to %v", common.DefaultSecretShares)
				common.SecShares = common.DefaultSecretShares
			}
		} else {
			log.Warnf("Secret Shares is not specified, defaulting to %d", common.DefaultSecretShares)
			common.SecShares = common.DefaultSecretShares
		}
		//	Secret Threshold
		if len(configMapObject.Data["secretThreshold"]) > 0 {
			common.SecThreshold, err = strconv.Atoi(strings.TrimSpace(configMapObject.Data["secretThreshold"]))
			if err != nil {
				log.Warnf("error while parsing the secret threshold input, defaulting to %v", common.DefaultSecretThreshold)
				common.SecThreshold = common.DefaultSecretThreshold
			}
		} else {
			log.Warnf("Secret Threshold is not specified, defaulting to %d", common.DefaultSecretThreshold)
			common.SecThreshold = common.DefaultSecretThreshold
		}
		//	Service Wait time for periodic checks of vault availability
		if len(configMapObject.Data["serviceWaitTimeInSeconds"]) > 0 {
			common.WaitTimeSeconds, err = strconv.Atoi(strings.TrimSpace(configMapObject.Data["serviceWaitTimeInSeconds"]))
			if err != nil {
				log.Warnf("error while parsing the wait time input, defaulting to %v", common.WaitTime)
				common.WaitTimeSeconds = common.WaitTime
			}
		} else {
			log.Warnf("Wait time is not specified, defaulting to %d", common.WaitTime)
			common.WaitTimeSeconds = common.WaitTime
		}

		//	LDAP Configurations
		if len(configMapObject.Data["enableLDAP"]) > 0 {
			enableLDAPJsonStr = configMapObject.Data["enableLDAP"]
		} else {
			log.Warnf("LDAP enable JSON payload is not provided, defaulting with basic enable payload")
			enableLDAPJsonStr = fmt.Sprintf(`{ "type": "ldap", "description": "Login with LDAP" }`)
		}

		if len(configMapObject.Data["ldapConfig"]) > 0 {
			ldapConfigString = configMapObject.Data["ldapConfig"]
		} else {
			log.Fatal("LDAP Configuration not found.")
		}

	}
}

// StartRoutine is the chore functionality of the process.
// i) populates the name of the individual pods and corresponding IPs
// ii) check for all the available IP addresses for the pods
// iii) Starts to initialize the vault on a single targeted IP address
// iv) Unseals the vault on the targeted IP address
// v) Enables LDAP
// vi) Configures LDAP
// vii) Writes the ACL policies
// viii) Enables the secret engines
// ix) continues the steps i, ii, iv in a loop to maintain the high-availability when new pods comes
// or existing pod crashes
func StartRoutine() {
	populatePodNameKeysAndIPs()
	checkIPAvailabilityForAllPods()
	startInitializingWithIndividualPodIP()
	checkSealStatus()
	configureLDAPInPod()
	writePolicyInPod()
	enableSecretEngineInPod()
	for {
		populatePodNameKeysAndIPs()
		checkIPAvailabilityForAllPods()
		checkSealStatus()
		time.Sleep(3 * time.Second)
	}
}

// populatePodNameKeysAndIPs populates the pod names that matches the given label selector
// and populates the available IP addresses for those corresponding pods
func populatePodNameKeysAndIPs() {
	pods, _ := k8s.Clientset.CoreV1().Pods(namespace).List(metaV1.ListOptions{
		LabelSelector: vaultLabelSelectors,
		FieldSelector: "status.phase=Running",
	})
	for _, pod := range pods.Items {
		podNameToIPMap[pod.Name] = pod.Status.PodIP
	}
	log.Debugf("Populated IP Map %v ", podNameToIPMap)
}

// checkIPAvailabilityForAllPods will extract the IP Addresses for all the pods that are
// collected in the populatePodNameKeysAndIPs function and makes sure the IP addresses are
// available for all; if the pod haven't got the ip assigned yet, it will wait untill all the
// pod ip are available
func checkIPAvailabilityForAllPods() {
	for podName, ip := range podNameToIPMap {
		if len(ip) == 0 {
			log.Warnf("No IP address found for %s", podName)
			populatePodNameKeysAndIPs()
			break
		}
	}
	log.Debugf("all ips available for identified pods")
}

// isInitializedOnPodIP will return bool value based on the initialized status on individual pod IP addresses
func isInitializedOnPodIP(firstPodName, initPodURL string) bool {
	initStatusResponse, err := FireRequest("", initPodURL, nil, common.HttpMethodGET)
	if err != nil {
		log.Errorf("error while checking the init status on %s; err: %v", initPodURL, err)
		return true
	} else {
		log.Debugf("Init response from pod %s is %v", firstPodName, string(initStatusResponse))
		var responseJSON map[string]interface{}
		json.Unmarshal(initStatusResponse, &responseJSON)
		if (responseJSON["initialized"]).(bool) {
			return true
		} else {
			return false
		}
	}
}

// checkSealStatus will validate the seal status on the individual pods against the pod IPs
// and unseals them if its sealed. This will unseal all the available pods that are coming up new
// or coming after a crashed pods and provides the high-availability
func checkSealStatus() {
	for podName, podIP := range podNameToIPMap {
		if isIndividualPodSealed(podName, podIP) {
			startUnsealingIndividualPod(podName, podIP)
		}
	}
}

// getFirstResponsivePod will get the first pod in the list of selected vault pods based on the labels
// provided. This is obtained via HEAD request to the pod ip on default port number 8200. The request is
// intentionally kept for 1 second wait time. as HEAD request should not take more time than that.
func getFirstResponsivePod() (firstPodName, firstPodIP string) {
	log.Info("entering to get the first responsive pod")
	for {
		for podName, podIP := range podNameToIPMap {
			client := http.Client{Timeout: time.Duration(common.WaitTimeSeconds) * time.Second}
			res, err := client.Head("http://" + strings.TrimSpace(podIP) + ":8200")
			if err != nil {
				log.Errorf("error from getFirstResponsivePod; service running in pod %s still not accepting the connection: %v moving to next pod", podName, err.Error())
				time.Sleep(1 * time.Second)
			} else {
				log.Infof("Service is live with status %v on pod %s", res.Status, podName)
				firstPodName = podName
				firstPodIP = podIP
				break
			}
		}
		if len(firstPodIP) > 0 && len(firstPodName) > 0 {
			break
		} else {
			continue
		}
		log.Info("no response from any pod, looping again")
	}
	return
}

// startUnsealingIndividualPod will start the unsealing process
func startUnsealingIndividualPod(podName, podIP string) {
	if !parsedKeys.IsEmpty() {
		unsealIndividualPods(podName, podIP)
	} else {
		populateParsedKeys()
		unsealIndividualPods(podName, podIP)
	}
}

// storeInSecret will store the initialized keys
func storeInSecret(parsedKeys common.VaultInitResp) error {
	jsonParsedKeys, err := json.Marshal(parsedKeys)
	if err != nil {
		return err
	}
	secretNew := &v1.Secret{
		ObjectMeta: metaV1.ObjectMeta{
			Name:   common.VaultKeysSecretName,
			Labels: map[string]string{"app": "vault", "type": "init-keys"},
		},
		StringData: map[string]string{"init-keys": string(jsonParsedKeys)},
		Type:       v1.SecretTypeOpaque,
	}
	_, err = k8s.Clientset.CoreV1().Secrets(namespace).Create(secretNew)
	if err != nil {
		return err
	}
	return nil
}

// bindPolicy will bind the given policy to the corresponding user or groups
func bindPolicy(nouns []string, policyPayload, url string) {
	for _, indNoun := range nouns {
		_, err := FireRequest(policyPayload, url+strings.TrimSpace(indNoun), getAuthTokenHeaders(), common.HttpMethodPUT)
		if err != nil {
			log.Errorf("error while uploading the policy binding: %v to group %s; error: %v", policyPayload, indNoun, err)
		}
	}
}

// getAuthTokenHeaders provides the auth token
func getAuthTokenHeaders() map[string]string {
	var authTokenHeaders map[string]string
	if !parsedKeys.IsEmpty() {
		authTokenHeaders = map[string]string{
			"X-Vault-Token": parsedKeys.RootToken,
		}
	} else {
		populateParsedKeys()
		authTokenHeaders = map[string]string{
			"X-Vault-Token": parsedKeys.RootToken,
		}
	}
	return authTokenHeaders
}

// populateParsedKeys will populate the parsed init-keys again from the secret; as the initializer pod crashes and comes up
// again this will not be in the session.
func populateParsedKeys() {
	keyObjectFromSecret, err := k8s.Clientset.CoreV1().Secrets(namespace).Get(common.VaultKeysSecretName, metaV1.GetOptions{})
	if err != nil {
		log.Fatalf("couldn't complete the unsealing process, as the secret keys cannot be obtained from k8s; err: %v", err)
	} else {
		parseJSONRespo(keyObjectFromSecret.Data["init-keys"], &parsedKeys)
	}
}
