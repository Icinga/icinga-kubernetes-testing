# Icinga Kubernetes Testing

## Parts

### Controller
Git-Repo: https://github.com/Icinga/icinga-kubernetes-testing
- Deployed when the ktesting module gets enabled
- API to deploy Tester Pods
- API to config tests
- Write tests to database

#### API endpoints

**/manage/create**
Parameters:
- n (int): Number of Tester Pods to create
- requestCpu (str): CPU request for each Tester Pod
- requestMemory (str): Memory request for each Tester Pod
- limitCpu (str): CPU limit for each Tester Pod
- limitMemory (str): Memory limit for each Tester Pod

**/manage/delete**
- names (list): Names of Tester Pods to delete separated by comma

**/manage/wipe**  
no parameters

### Tester
Git-Repo: https://github.com/Icinga/icinga-kubernetes-testing
- Read tests from database
  - On start and then periodically
- Run tests

### Icingaweb2 Module
Git-Repo: https://github.com/Icinga/icinga-kubernetes-testing-web
- List/Create/Manage Tester Pods 
- Start/End Tests

## Concepts

### How to make tests permanent
- Database to store which pod should run which test

### List testing pods and more information
- Namespace 'testing'
- Controller Pod is named/labeled with 'icinga-kubernetes-testing-controller'
- Tester Pods are named/labeled with 'icinga-kubernetes-testing-tester'

### How does Web communicate with Tester Pods
- Web calls Controller API
- Controller knows all Tester Pods
- Controller writes to database
- Tester Pods read from database (On start and then periodically)

### How to deploy the testing pods via Web
- Web calls Controller API to deploy Tester Pods
- Requests and limits can be set via API
