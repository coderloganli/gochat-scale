// Authentication Helper Functions
import http from 'k6/http';
import { baseUrl } from './config.js';

// Register a new user and return authToken
export function registerUser(userName, password) {
  const payload = JSON.stringify({
    userName: userName,
    passWord: password,
  });

  const params = {
    headers: { 'Content-Type': 'application/json' },
    tags: { name: 'register' },
  };

  const res = http.post(`${baseUrl}/user/register`, payload, params);

  if (res.status === 200) {
    try {
      const body = JSON.parse(res.body);
      if (body.code === 0 && body.data) {
        return body.data; // authToken
      }
    } catch (e) {
      // Parse error, return null
    }
  }
  return null;
}

// Login user and return authToken
export function loginUser(userName, password) {
  const payload = JSON.stringify({
    userName: userName,
    passWord: password,
  });

  const params = {
    headers: { 'Content-Type': 'application/json' },
    tags: { name: 'login' },
  };

  const res = http.post(`${baseUrl}/user/login`, payload, params);

  if (res.status === 200) {
    try {
      const body = JSON.parse(res.body);
      if (body.code === 0 && body.data) {
        return body.data; // authToken
      }
    } catch (e) {
      // Parse error, return null
    }
  }
  return null;
}

// Check if authToken is valid
export function checkAuth(authToken) {
  const payload = JSON.stringify({
    authToken: authToken,
  });

  const params = {
    headers: { 'Content-Type': 'application/json' },
    tags: { name: 'checkAuth' },
  };

  const res = http.post(`${baseUrl}/user/checkAuth`, payload, params);

  if (res.status === 200) {
    try {
      const body = JSON.parse(res.body);
      return body.code === 0;
    } catch (e) {
      return false;
    }
  }
  return false;
}

// Logout user
export function logoutUser(authToken) {
  const payload = JSON.stringify({
    authToken: authToken,
  });

  const params = {
    headers: { 'Content-Type': 'application/json' },
    tags: { name: 'logout' },
  };

  const res = http.post(`${baseUrl}/user/logout`, payload, params);

  if (res.status === 200) {
    try {
      const body = JSON.parse(res.body);
      return body.code === 0;
    } catch (e) {
      return false;
    }
  }
  return false;
}

// Get or create a test user with authToken
// Uses VU ID to ensure unique users per virtual user
export function getAuthToken(vuId) {
  const timestamp = Date.now();
  const userName = `loadtest_user_${vuId}_${timestamp}`;
  const password = 'loadtest123';

  // Try to register new user
  let authToken = registerUser(userName, password);

  if (!authToken) {
    // Registration might fail if user exists, try login
    authToken = loginUser(userName, password);
  }

  return {
    authToken,
    userName,
    password,
  };
}

// Create multiple test users for setup phase
export function createTestUsers(count, prefix = 'loadtest') {
  const users = [];
  const timestamp = Date.now();

  for (let i = 0; i < count; i++) {
    const userName = `${prefix}_${i}_${timestamp}`;
    const password = 'loadtest123';

    const authToken = registerUser(userName, password);

    if (authToken) {
      users.push({
        userName,
        password,
        authToken,
      });
    }
  }

  console.log(`Created ${users.length}/${count} test users`);
  return users;
}
