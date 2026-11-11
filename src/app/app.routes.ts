import { Routes } from '@angular/router';
import { Dashboard } from './dashboard/dashboard';

export const routes: Routes = [
  { path: '', component: Dashboard, title: 'GESTOCK · SIGAD' },
  { path: '**', redirectTo: '' },
];
