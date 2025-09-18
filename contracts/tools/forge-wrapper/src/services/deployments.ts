import { listNetworkNames } from "./networks";
import { loadDeployments } from "../deployments";

export interface DeploymentSummary {
  network: string;
  alias: string;
  address: string;
  deployedAt: string;
}

export async function listDeployments(customer: string, network?: string): Promise<DeploymentSummary[]> {
  const networks = network ? [network] : await listNetworkNames();
  const summaries: DeploymentSummary[] = [];
  for (const net of networks) {
    const file = await loadDeployments(customer, net);
    for (const [alias, record] of Object.entries(file.current)) {
      summaries.push({
        network: net,
        alias,
        address: record.address,
        deployedAt: record.deployedAt,
      });
    }
  }
  return summaries.sort((a, b) => a.network.localeCompare(b.network) || a.alias.localeCompare(b.alias));
}
