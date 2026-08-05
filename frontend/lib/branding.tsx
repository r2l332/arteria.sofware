'use client';

import { createContext, useContext, useState, useEffect, ReactNode } from 'react';

export interface Branding {
  org_id: string | null;
  app_name: string;
  subtitle: string;
  logo_url: string;
  primary_color: string;
  accent_color: string;
  favicon_url: string;
  support_email: string;
  slug?: string;
  org_name?: string;
}

const defaultBranding: Branding = {
  org_id: null,
  app_name: 'Arteria',
  subtitle: 'Integration Engine',
  logo_url: '',
  primary_color: '#6366f1',
  accent_color: '#6366f1',
  favicon_url: '',
  support_email: '',
};

const BrandingContext = createContext<Branding>(defaultBranding);

export function BrandingProvider({ children }: { children: ReactNode }) {
  const [branding, setBranding] = useState<Branding>(defaultBranding);

  useEffect(() => {
    const base = typeof window !== 'undefined' ? `${window.location.origin}/api/v1` : '/api/v1';
    fetch(`${base}/branding`)
      .then(r => r.json())
      .then((data: Branding) => {
        setBranding(data);

        // Apply CSS custom properties for theming
        if (data.primary_color) {
          document.documentElement.style.setProperty('--brand-primary', data.primary_color);
          document.documentElement.style.setProperty('--brand-accent', data.accent_color || data.primary_color);
        }

        // Set page title
        if (data.app_name) {
          document.title = `${data.app_name} — ${data.subtitle || 'Integration Engine'}`;
        }

        // Set favicon
        if (data.favicon_url) {
          const link = document.querySelector("link[rel~='icon']") as HTMLLinkElement
            || document.createElement('link');
          link.rel = 'icon';
          link.href = data.favicon_url;
          document.head.appendChild(link);
        }
      })
      .catch(() => {});
  }, []);

  return (
    <BrandingContext.Provider value={branding}>
      {children}
    </BrandingContext.Provider>
  );
}

export function useBranding() {
  return useContext(BrandingContext);
}
