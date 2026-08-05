import type { Metadata } from 'next';
import './globals.css';
import { AuthGate } from '@/components/AuthGate';
import { BrandingProvider } from '@/lib/branding';

export const metadata: Metadata = {
  title: 'Arteria — Integration Engine',
  description: 'High-throughput HL7v2/FHIR/DICOM interoperability engine',
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en" className="dark">
      <head>
        <link rel="preconnect" href="https://fonts.googleapis.com" />
        <link rel="preconnect" href="https://fonts.gstatic.com" crossOrigin="anonymous" />
        <link href="https://fonts.googleapis.com/css2?family=Inter:wght@400;500;600;700&family=JetBrains+Mono:wght@400;500&display=swap" rel="stylesheet" />
      </head>
      <body className="antialiased">
        <BrandingProvider>
          <AuthGate>{children}</AuthGate>
        </BrandingProvider>
      </body>
    </html>
  );
}
