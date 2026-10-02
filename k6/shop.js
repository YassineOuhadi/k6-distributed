import http from 'k6/http';
import { check, sleep } from 'k6';

const BASE_URL = __ENV.BASE_URL || 'http://localhost:8080';
const PRODUCTS = ['p-1', 'p-2', 'p-3', 'p-4', 'p-5'];

// browse, order, read the order back
export function placeOrder() {
  const list = http.get(`${BASE_URL}/api/products`, { tags: { name: 'list_products' } });
  check(list, { 'products 200': (r) => r.status === 200 });

  const product = PRODUCTS[Math.floor(Math.random() * PRODUCTS.length)];
  const res = http.post(
    `${BASE_URL}/api/orders`,
    JSON.stringify({ product_id: product, quantity: 1 }),
    { headers: { 'Content-Type': 'application/json' }, tags: { name: 'create_order' } },
  );
  check(res, {
    'order 201': (r) => r.status === 201,
    'order confirmed': (r) => r.json('status') === 'confirmed',
  });

  if (res.status === 201) {
    const get = http.get(`${BASE_URL}/api/orders/${res.json('id')}`, { tags: { name: 'get_order' } });
    check(get, { 'get order 200': (r) => r.status === 200 });
  }
  sleep(1);
}
