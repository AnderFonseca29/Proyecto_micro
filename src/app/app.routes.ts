import { Routes } from '@angular/router';
import { Dashboard } from './dashboard/dashboard';

export const routes: Routes = [
  { path: '', component: Dashboard, title: 'GESTOCK · Panel' },
  {
    path: 'recepcion',
    loadComponent: () => import('./recepcion/recepcion').then((m) => m.Recepcion),
    title: 'GESTOCK · Recepción / Ingresos',
  },
  { path: '**', redirectTo: '' },
];
