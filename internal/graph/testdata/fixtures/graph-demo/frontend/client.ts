export interface Client {
  fetchOrders(): Promise<string[]>;
}

export class Base {
  get(path: string): Promise<string> {
    return Promise.resolve("");
  }
}
