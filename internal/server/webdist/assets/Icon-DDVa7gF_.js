import{f as u}from"./format-length-B-p6aW7q.js";import{aT as v,bH as l,aS as m,d as b,aU as h,et as y,aN as d,aV as C,aX as _,bN as $,g as a,fA as z}from"./index-B7tniPFu.js";var S=v("icon",`
 height: 1em;
 width: 1em;
 line-height: 1em;
 text-align: center;
 display: inline-block;
 position: relative;
 fill: currentColor;
`,[l("color-transition",{transition:"color .3s var(--n-bezier)"}),l("depth",{color:"var(--n-color)"},[m("svg",{opacity:"var(--n-opacity)",transition:"opacity .3s var(--n-bezier)"})]),m("svg",{height:"1em",width:"1em"})]);const x={...h.props,depth:[String,Number],size:[Number,String],color:String,component:[Object,Function]},P=b({_n_icon__:!0,name:"Icon",inheritAttrs:!1,props:x,setup(e){const{mergedClsPrefixRef:t,inlineThemeDisabled:n}=_(e),o=h("Icon","-icon",S,z,e,t),i=a(()=>{const{depth:r}=e,{common:{cubicBezierEaseInOut:c},self:p}=o.value;if(r!==void 0){const{color:f,[`opacity${r}Depth`]:g}=p;return{"--n-bezier":c,"--n-color":f,"--n-opacity":g}}return{"--n-bezier":c,"--n-color":"","--n-opacity":""}}),s=n?$("icon",a(()=>`${e.depth||"d"}`),i,e):void 0;return{mergedClsPrefix:t,mergedStyle:a(()=>{const{size:r,color:c}=e;return{fontSize:u(r),color:c}}),cssVars:n?void 0:i,themeClass:s?.themeClass,onRender:s?.onRender}},render(){const{$parent:e,depth:t,mergedClsPrefix:n,component:o,onRender:i,themeClass:s}=this;return e?.$options?._n_icon__&&y("icon","don't wrap `n-icon` inside `n-icon`"),i?.(),d("i",C(this.$attrs,{role:"img",class:[`${n}-icon`,s,{[`${n}-icon--depth`]:t,[`${n}-icon--color-transition`]:t!==void 0}],style:[this.cssVars,this.mergedStyle]}),o?d(o):this.$slots.default?.())}});export{P as N};
