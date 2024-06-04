import ReactDOM from 'react-dom';
import App from './App';
import './index.css';

// hack for buffer error on react-scripts version > 5
// eslint-disable-next-line
window.Buffer = window.Buffer || require('buffer').Buffer;

ReactDOM.render(<App />, document.getElementById('root'));
