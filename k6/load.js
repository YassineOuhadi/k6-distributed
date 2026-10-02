import { placeOrder } from './shop.js';

// VUs are split across runner pods by k6-operator (parallelism)
export const options = {
  stages: [
    { duration: '1m', target: 40 },
    { duration: '3m', target: 40 },
    { duration: '30s', target: 0 },
  ],
  thresholds: {
    http_req_failed: ['rate<0.01'],
    'http_req_duration{name:create_order}': ['p(95)<500'],
    checks: ['rate>0.99'],
  },
};

export default placeOrder;
