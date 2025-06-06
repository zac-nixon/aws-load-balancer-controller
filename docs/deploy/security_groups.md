# Security Groups for Load Balancers

Use security groups to limit client connections to your load balancers, and restrict connections with nodes. The AWS Load Balancer Controller (LBC) defines two classifications of security groups: **frontend** and **backend**.

- **Frontend Security Groups:** Determine the clients that can access the load balancers.
- **Backend Security Groups:** Permit the load balancer to connect to targets, such as EC2 instances or ENIs.

## Frontend Security Groups

Frontend security groups control access to load balancers by specifying which clients can connect to them.

Use cases for Frontent Security Groups include:

* Placing the load balancer behind another service, such as [AWS Web Application Firewall](https://docs.aws.amazon.com/waf/latest/developerguide/what-is-aws-waf.html) or [AWS CloudFront](https://docs.aws.amazon.com/AmazonCloudFront/latest/DeveloperGuide/Introduction.html).
* Blocking the IP address range (CIDR) of a region.
* Configuring the Load Balancer for private or internal use, by specifying internal CIDRs and Security Groups. 

In the default configuration, the LBC automatically creates one security group per load balancer, allowing traffic from `inbound-cidrs` to `listen-ports`.

### Configuration

Apply custom frontend security groups with an annotation. This disables automatic generation of frontend security groups. 

- For Ingress resources, use the [`alb.ingress.kubernetes.io/security-groups`](../guide/ingress/annotations.md#security-groups) annotation.
- For Service resources, use the [`service.beta.kubernetes.io/aws-load-balancer-security-groups`](../guide/service/annotations.md#security-groups) annotation.
- The annotation must be set to one or more security group IDs or security group names.


## Backend Security Groups

Backend Security Groups control traffic between AWS Load Balancers and their target EC2 instances or ENIs. For example, backend security groups can restrict the ports load balancers may access on nodes.

- Backend security groups permit traffic from AWS Load Balancers to their targets. 
- LBC uses a single, shared backend security group, attaching it to each load balancer and using as the traffic source in the security group rules it adds to targets.
- When configuring security group rules at the ENI/Instance level, use the Security Group ID of the backend security group. Avoid using the IP addresses of a specific AWS Load Balancer, these IPs are dynamic and the security group rules aren't updated automatically.

### Configuration

**Enable or Disable:** Use `--enable-backend-security-group` (default `true`) to enable/disable the shared backend security group.

Note that while you can turn off the shared backend security group feature by setting it to `false`, if you have a high number of Ingress resources with frontend security groups auto-generated by the controller, you might run into security group rule limits on the instance/ENI security groups.

**Specification:** Use `--backend-security-group` to pass in a security group ID to use as a custom shared backend security group. 

**Important Notes:**
* The Custom Shared Backend Security Group (`--backend-security-group` option) only works when the automatic addition of Inbound Rules to the Node/ENI Security Group is enabled.
* If a Custom Frontend Security Group is configured, you must set the annotation `service.beta.kubernetes.io/aws-load-balancer-manage-backend-security-group-rules: "true"` for the Custom Shared Backend Security Group to work correctly.

If `--backend-security-group` is left empty, a security group with the following attributes will be created:

  ```yaml
  name: k8s-traffic-<cluster_name>-<hash_of_cluster_name>
  tags: 
      elbv2.k8s.aws/cluster: <cluster_name>
      elbv2.k8s.aws/resource: backend-sg
  ```


### Coordination of Frontend and Backend Security Groups


- If the LBC auto-creates the frontend security group for a load balancer, it automatically adds the security group rules to allow traffic from the load balancer to the backend instances/ENIs.
- If the frontend security groups are manually specified, the LBC will not **by default** add any rules to the backend security group.

#### Enable Autogeneration of Backend Security Group Rules

- If using custom frontend security groups, the LBC can be configured to automatically manage backend security group rules.
- To enable managing backend security group rules, apply an additional annotation to Ingress and Service resources.
    - For Ingress resources, set the `alb.ingress.kubernetes.io/manage-backend-security-group-rules` annotation to `true`.
    - For Service resources, set the `service.beta.kubernetes.io/aws-load-balancer-manage-backend-security-group-rules` annotation to `true`.
- If management of backend security group rules is enabled with an annotation on a Service or Ingress, then `--enable-backend-security-group` must be set to true.
- These annotations are ignored when using auto-generated frontend security groups. 
- To enable managing backend security group rules for all resources, using cli flag `--enable-manage-backend-security-group-rules`
    - when set to `true`, the controller will automatically manage backend security group rules for all resources
    - individual annotation takes precedence over cli flag, meaning it can be overridden with annotation `alb.ingress.kubernetes.io/manage-backend-security-group-rules: "false"` for ALB or `service.beta.kubernetes.io/aws-load-balancer-manage-backend-security-group-rules: "false"` for NLB
    - for this to take effect, `--enable-backend-security-group` needs to be true and user explicitly specify security group using annotation: `alb.ingress.kubernetes.io/security-groups` or `service.beta.kubernetes.io/aws-load-balancer-manage-backend-security-group-rules`
    - when set to `false` (default value) or not set, the controller takes the individual annotations
  
### Port Range Restrictions

From version v2.3.0 onwards, the controller restricts port ranges in the backend security group rules by default. This improves the security of the default configuration. The LBC should generate the necessary rules to permit traffic, based on the Service and Ingress resources. 

If needed, set the controller flag `--disable-restricted-sg-rules` to `true` to permit traffic to all ports. This may be appropriate for backwards compatability, or troubleshooting. 
