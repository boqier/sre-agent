package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"time"

	"gopkg.in/yaml.v2"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"

	//v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

func main() {
	client, err := getKubernetesClient()
	if err != nil {
		log.Fatalf("Failed to create Kubernetes client: %v", err)
	}

	err = agentLoop(client)
	if err != nil {
		log.Fatalf("Agent loop failed: %v", err)
	}
}
func getDeploymentYAML(clientset *kubernetes.Clientset, namespace, podName string) (string, string, error) {
	// 1️⃣ 获取 Pod
	pod, err := clientset.CoreV1().Pods(namespace).Get(context.TODO(), podName, metav1.GetOptions{})
	if err != nil {
		return "", "", fmt.Errorf("failed to get pod %s: %w", podName, err)
	}

	// 2️⃣ 找 Pod 的 ReplicaSet
	rsName := ""
	for _, o := range pod.OwnerReferences {
		if o.Kind == "ReplicaSet" {
			rsName = o.Name
			break
		}
	}
	if rsName == "" {
		return "", "", fmt.Errorf("pod %s 没有上层 ReplicaSet", podName)
	}

	// 3️⃣ 找 ReplicaSet 对应的 Deployment
	rs, err := clientset.AppsV1().ReplicaSets(namespace).Get(context.TODO(), rsName, metav1.GetOptions{})
	if err != nil {
		return "", "", fmt.Errorf("failed to get replicaset %s: %w", rsName, err)
	}

	deployName := ""
	for _, o := range rs.OwnerReferences {
		if o.Kind == "Deployment" {
			deployName = o.Name
			break
		}
	}
	if deployName == "" {
		return "", "", fmt.Errorf("replicaset %s 没有上层 Deployment", rsName)
	}

	// 4️⃣ 获取 Deployment
	deploy, err := clientset.AppsV1().Deployments(namespace).Get(context.TODO(), deployName, metav1.GetOptions{})
	if err != nil {
		return "", "", fmt.Errorf("failed to get deployment %s: %w", deployName, err)
	}

	// 5️⃣ 转成 map 并清理不必要字段
	yamlStr, err := getCleanDeploymentYAML(deploy)
	if err != nil {
		return "", "", fmt.Errorf("failed to clean deployment yaml: %w", err)
	}

	return yamlStr, deployName, nil
}
func cleanDeploymentYAML(raw interface{}) interface{} {
	if m, ok := raw.(map[string]interface{}); ok {
		delete(m, "status")
		delete(m, "resourceVersion")
		delete(m, "uid")
		delete(m, "creationTimestamp")
		delete(m, "generation")
		delete(m, "managedFields")
		// 可选：删除 annotation
		delete(m, "annotations")

		for k, v := range m {
			m[k] = cleanDeploymentYAML(v)
		}
	} else if arr, ok := raw.([]interface{}); ok {
		for i, v := range arr {
			arr[i] = cleanDeploymentYAML(v)
		}
	}
	return raw
}

func getCleanDeploymentYAML(deploy interface{}) (string, error) {
	data, err := json.Marshal(deploy)
	if err != nil {
		return "", err
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return "", err
	}

	cleaned := cleanDeploymentYAML(raw)
	yamlData, err := yaml.Marshal(cleaned)
	if err != nil {
		return "", err
	}

	return string(yamlData), nil
}

func getKubernetesClient() (*kubernetes.Clientset, error) {
	//kubeconfig := filepath.Join(os.Getenv("HOME"), ".kube", "config")
	//从集群中获取config
	config, err := rest.InClusterConfig()
	if err != nil {
		kubeconfig := "./k3s.yaml"
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			return nil, fmt.Errorf("error building kubeconfig: %w", err)
		}
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("error creating clientset: %w", err)
	}
	return clientset, nil
}
func getLLMDecision(namespace, deploymentYaml, reason string) (string, string, error) {

	userprompt := fmt.Sprintf(`你是一个云原生技术专家，负责分析 Kubernetes Pod/Deployment 出现的错误，并生成可直接修复问题的方案。请严格按照以下规则返回内容：
1. 如果问题可以通过重启 Pod 解决，请只输出 restart。
2. 如果问题可以通过修改 Deployment 生成 patch JSON 解决，请直接输出可用于 kubectl patch 的 JSON，不允许任何解释或额外文字,不要带代码框。
3. 如果问题严重，需要人工干预（如数据库连接错误、服务 500 错误），请解释问题原因,以及错误的的信息如命名空间,deployment等,并在输出中添加 [feishu] 字段。

K8s 报错信息：
%s

对应的 Deployment YAML：
%s`, reason, deploymentYaml)

	Openaiclent, err := NewOpenAIClient()
	if err != nil {
		return "", "", err
	}

	resp, err := Openaiclent.SendMessage("你是一个Kubernetes专家,帮助解决集群的问题", userprompt)
	if err != nil {
		return "", "", err
	}

	responseText := strings.ToLower(resp) // 转换为小写
	if strings.Contains(responseText, "restart") {
		log.Println("LLM response contains 'restart'. Interpreting as RESTART action.")
		return "RESTART", "重启了", nil
	}
	if strings.Contains(responseText, "[feishu]") {
		log.Println("LLM response contains '[feishu]'. Interpreting as DANGEROUS action.")
		return "DANGEROUS", responseText, nil
	}

	return "PATCH", responseText, nil
	// ---------------------
}
func executeAction(clientset *kubernetes.Clientset, namespace, podName, deploymentName, action string, message string) error {
	switch action {
	case "RESTART":
		if err := restartPod(clientset, namespace, podName); err != nil {
			return err
		}
		log.Printf("Deleted pod %s/%s to trigger restart.", namespace, podName)
		return nil
	case "DANGEROUS":
		if err := sendFeishuAlert(message, "https://www.feishu.cn/flow/api/trigger-webhook/7f016c94474f2681b672311cb70607d1"); err != nil {
			return err
		}
		log.Println("Sent Feishu alert for dangerous issue.")
		return nil
	case "PATCH":
		if err := applyPatch(clientset, namespace, deploymentName, message); err != nil {
			return err
		}
		log.Println("Applied patch to fix the issue.")
		return nil
	default:
		return fmt.Errorf("unknown action: %s", action)

	}

}
func restartPod(clientset *kubernetes.Clientset, namespace, podName string) error {
	return clientset.CoreV1().Pods(namespace).Delete(context.TODO(), podName, metav1.DeleteOptions{})
}
func applyPatch(clientset *kubernetes.Clientset, namespace, deploymentName, patchJSON string) error {
	fmt.Printf("apply patch: %s", patchJSON)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	patchStr := strings.TrimSpace(patchJSON)
	if patchStr == "" {
		return fmt.Errorf("empty patch")
	}
	// 基本 JSON 校验（防止明显的语法错误）
	var tmp interface{}
	if err := json.Unmarshal([]byte(patchStr), &tmp); err != nil {
		return fmt.Errorf("patch is not valid JSON: %w", err)
	}

	// 1) dry-run patch
	dryRunOpts := metav1.PatchOptions{DryRun: []string{metav1.DryRunAll}}
	_, err := clientset.AppsV1().Deployments(namespace).Patch(ctx, deploymentName, types.StrategicMergePatchType, []byte(patchStr), dryRunOpts)
	if err != nil {
		// dry-run 出错，返回带说明的错误（不执行真实 patch）
		fmt.Println(err)
		return fmt.Errorf("dry-run patch failed: %w", err)
	}
	// 2) dry-run 成功，执行真实 patch
	_, err = clientset.AppsV1().Deployments(namespace).Patch(ctx, deploymentName, types.StrategicMergePatchType, []byte(patchStr), metav1.PatchOptions{})
	if err != nil {
		return fmt.Errorf("apply patch failed after successful dry-run: %w", err)
	}
	message := "集群有一条新的变更：Kubernetes Deployment " + deploymentName + " in namespace " + namespace + " has been patched to fix an issue."
	sendFeishuAlert(message, "https://www.feishu.cn/flow/api/trigger-webhook/6c7c623ec2cbc257e0ee37c0bfa0aa1d")
	return nil
}

func sendFeishuAlert(message string, webhookURL string) error {
	type FeishuMessage struct {
		MsgType string `json:"msg_type"`
		Content struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	client := &http.Client{}
	var feishuMsg FeishuMessage
	feishuMsg.MsgType = "text"
	feishuMsg.Content.Text = message
	payloadBytes, err := json.Marshal(feishuMsg)
	if err != nil {
		return fmt.Errorf("failed to marshal Feishu message: %w", err)
	}
	req, err := http.NewRequest("POST", webhookURL, bytes.NewBuffer(payloadBytes))
	if err != nil {
		return fmt.Errorf("failed to create HTTP request: %w", err)
	}
	res, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send HTTP request: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(res.Body)
		return fmt.Errorf("Feishu API returned non-200 status: %d, body: %s", res.StatusCode, body)
	}
	fmt.Print("发送飞书成功！")
	return nil
}
func agentLoop(client *kubernetes.Clientset) error {
	for {
		log.Println("--- Agent Loop Start ---")

		// 1. 感知：查询Prometheus告警
		alerts, err := queryPrometheusAlerts()
		if err != nil {
			log.Printf("Error querying Prometheus: %v", err)
			time.Sleep(1 * time.Minute) // 如果查询失败，等待更长时间
			continue
		}

		if len(alerts) == 0 {
			log.Println("No active alerts. System is healthy.")
		} else {
			log.Printf("Found %d active alert(s).", len(alerts))
			// 2. 思考与行动：处理每个告警
			for _, alert := range alerts {
				fmt.Println(alert.Labels["pod"])
				fmt.Println(alert.Annotations["description"])
				//限制测试条件
				if alert.Labels["alertname"] == "PodCrashLooping" || alert.Labels["alertname"] == "OOMKilled" || alert.Labels["alertname"] == "ImagePullBackOff" {
					Namespcae := alert.Labels["namespace"]
					podName := alert.Labels["pod"]
					reason := alert.Annotations["description"]
					yamlData, deploymentName, err := getDeploymentYAML(client, Namespcae, podName)
					if err != nil {
						log.Fatalf("Failed to get Deployment YAML: %v", err)
						return err
					}
					decision, patchYaml, err := getLLMDecision(Namespcae, yamlData, reason)
					executeAction(client, Namespcae, podName, deploymentName, decision, patchYaml)
					if err != nil {
						log.Fatalf("Failed to get LLM decision: %v", err)
						return err
					}
				}
			}
		}

		log.Println("--- Agent Loop End ---")
		time.Sleep(30 * time.Second) // 每30秒检查一次
		return nil
	}
}
