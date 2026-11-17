// The fixture's TypeScript half — the second grammar (T3) proves the
// registry was not shaped around Go: this file alone exercises inherits
// (extends), mixes_in (implements), imports and calls.
import { Base, Client } from "./client";

export class ApiClient extends Base implements Client {
  fetchOrders(): Promise<string[]> {
    return this.get("/orders").then(parseOrders);
  }
}

function parseOrders(body: string): string[] {
  return JSON.parse(body) as string[];
}

export function loadOrders(client: Client): Promise<string[]> {
  return client.fetchOrders();
}
