import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom';
import { Login } from './pages/Login';
import { Dashboard } from './pages/Dashboard';
import { Clan } from './pages/Clan';
import { Admin } from './pages/Admin';

// Declare the global admin mode flag injected by the server
declare global {
  interface Window {
    __ADMIN_MODE__?: boolean;
  }
}

function App() {
  // Check if admin mode is enabled (injected by the Go server)
  const isAdminMode = window.__ADMIN_MODE__ === true;

  if (isAdminMode) {
    return (
      <BrowserRouter>
        <Routes>
          <Route path="/" element={<Admin />} />
          <Route path="*" element={<Navigate to="/" replace />} />
        </Routes>
      </BrowserRouter>
    );
  }

  return (
    <BrowserRouter>
      <Routes>
        <Route path="/" element={<Login />} />
        <Route path="/dashboard" element={<Dashboard />} />
        <Route path="/clan" element={<Clan />} />
        <Route path="*" element={<Navigate to="/" replace />} />
      </Routes>
    </BrowserRouter>
  );
}

export default App;
