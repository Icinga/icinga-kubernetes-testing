# Icinga Kubernetes Testing

## Parts

### Controller
Git-Repo: https://github.com/Icinga/icinga-kubernetes-testing
- Watch for test resource changes in Kubernetes
- Create the correct resource for the test (One of Deployment, StatefulSet, DaemonSet, ReplicaSet)
- Ensure that the correct number of tester pods are running
- Send test configuration to tester pods via tcp connection

### API
Git-Repo: https://github.com/Icinga/icinga-kubernetes-testing
- Listen to port 8080 for http connection
- Create/Delete tests via API

#### API endpoints

**/manage/create**
Parameters:
- resourceType (str): Type of resource to test on (One of Deployment, StatefulSet, DaemonSet, ReplicaSet)
- resourceName (str): Name of the resource to test on
- description (str): Description of the test
- expectedPods (int): Number of Pods expected to be running
- tests (list): List of tests to run
  - tests[].kind (str): Type of test (e.g. cpu, memory, etc.)
  - tests[].percentage (int): Intensity of test in percent

**/manage/delete**
- tests (list): Tests to delete seperated by comma
  - tests[].namespace (str): Namespace of the test
  - tests[].name (str): Name of the test ('*' to delete all tests in given namespace)


### Tester
Git-Repo: https://github.com/Icinga/icinga-kubernetes-testing
- Listen to port 8080 for tcp connection
- Get yaml configuration from tcp connection 
- Run test specified in the configuration

### Icingaweb2 Module
Git-Repo: https://github.com/Icinga/icinga-kubernetes-testing-web
- Create/Delete tests via API
- Show information about tests and the depending pods
- Manage templates to create tests out of it
